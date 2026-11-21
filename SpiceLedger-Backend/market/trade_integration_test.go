package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asif-Faizal/SpiceLedger-Backend/util"
	"github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
	"github.com/segmentio/ksuid"
	"github.com/shopspring/decimal"
)

func TestRetryableMySQLErrors(t *testing.T) {
	if !isDeadlock(&mysql.MySQLError{Number: 1213}) {
		t.Fatal("deadlock should be retryable")
	}
	if !isLockWaitTimeout(&mysql.MySQLError{Number: 1205}) {
		t.Fatal("lock wait should be retryable")
	}
	if isDeadlock(&mysql.MySQLError{Number: 1062}) {
		t.Fatal("duplicate key is not a deadlock")
	}
}

type fixture struct {
	t       *testing.T
	db      *sql.DB
	svc     Service
	userID  string
	gradeID string
}

func (f *fixture) buy(qty, price, date, key string) *Transaction {
	f.t.Helper()
	txn, err := f.svc.Buy(context.Background(), TradeRequest{
		UserID: f.userID, SpiceGradeID: f.gradeID,
		Quantity: d(qty), Price: d(price), ClientTradeDate: date, IdempotencyKey: key,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return txn
}

func (f *fixture) sell(qty, price, date, key string) (*Transaction, error) {
	f.t.Helper()
	return f.svc.Sell(context.Background(), TradeRequest{
		UserID: f.userID, SpiceGradeID: f.gradeID,
		Quantity: d(qty), Price: d(price), ClientTradeDate: date, IdempotencyKey: key,
	})
}

func (f *fixture) position() *PositionView {
	f.t.Helper()
	pos, err := f.svc.GetGradePosition(context.Background(), f.userID, f.gradeID)
	if err != nil {
		f.t.Fatal(err)
	}
	return pos
}

func (f *fixture) countTx(kind string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM transactions WHERE user_id = ? AND type = ?`, f.userID, kind).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestCanonicalFIFOPersisted(t *testing.T) {
	f := newFixture(t)
	f.buy("100", "100", "2026-01-01", "")
	f.buy("50", "110", "2026-01-02", "")
	f.buy("80", "120", "2026-01-03", "")
	sold, err := f.sell("120", "150", "2026-01-04", "")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := f.db.Query(`
		SELECT b.id, sa.quantity, sa.realized_pnl
		FROM sell_allocations sa
		JOIN buy_lots b ON b.id = sa.buy_lot_id
		WHERE sa.sell_transaction_id = ?
		ORDER BY b.trade_date, b.id`, sold.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type alloc struct {
		lotID string
		qty   decimal.Decimal
		pnl   decimal.Decimal
	}
	var got []alloc
	for rows.Next() {
		var row alloc
		if err := rows.Scan(&row.lotID, &row.qty, &row.pnl); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].qty.Equal(d("100")) || !got[0].pnl.Equal(d("5000")) || !got[1].qty.Equal(d("20")) || !got[1].pnl.Equal(d("800")) {
		t.Fatalf("allocations = %+v", got)
	}

	var lotC decimal.Decimal
	if err := f.db.QueryRow(`
		SELECT remaining_qty FROM buy_lots
		WHERE user_id = ? AND price = 120 AND trade_date = '2026-01-03'`, f.userID).Scan(&lotC); err != nil {
		t.Fatal(err)
	}
	if !lotC.Equal(d("80")) {
		t.Fatalf("lot C remaining %s", lotC)
	}

	pos := f.position()
	if !pos.TotalQty.Equal(d("110")) || !pos.TotalCost.Equal(d("12900")) || !pos.RealizedPnL.Equal(d("5800")) {
		t.Fatalf("position qty %s cost %s pnl %s", pos.TotalQty, pos.TotalCost, pos.RealizedPnL)
	}
	if !pos.AvgCost.Equal(d("117.27272727")) {
		t.Fatalf("avg cost %s", pos.AvgCost)
	}
}

func TestFractionalBuySell(t *testing.T) {
	f := newFixture(t)
	f.buy("0.1", "100.25", "2026-02-01", "")
	f.buy("0.2", "101.50", "2026-02-02", "")
	if _, err := f.sell("0.15", "150.75", "2026-02-03", ""); err != nil {
		t.Fatal(err)
	}
	pos := f.position()
	// Remaining is 0.15 kg of the second lot at 101.50 = 15.225. Realized = 7.5125.
	if !pos.TotalQty.Equal(d("0.15")) || !pos.TotalCost.Equal(d("15.225")) || !pos.RealizedPnL.Equal(d("7.5125")) {
		t.Fatalf("qty %s cost %s pnl %s", pos.TotalQty, pos.TotalCost, pos.RealizedPnL)
	}
	var remaining, allocQty, allocPnL decimal.Decimal
	if err := f.db.QueryRow(`
		SELECT remaining_qty FROM buy_lots WHERE user_id = ? AND price = 101.50`, f.userID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if !remaining.Equal(d("0.15")) {
		t.Fatalf("second lot remaining %s", remaining)
	}
	if err := f.db.QueryRow(`
		SELECT quantity, realized_pnl FROM sell_allocations
		WHERE buy_lot_id = (SELECT id FROM buy_lots WHERE user_id = ? AND price = 100.25)`, f.userID).Scan(&allocQty, &allocPnL); err != nil {
		t.Fatal(err)
	}
	if !allocQty.Equal(d("0.1")) || !allocPnL.Equal(d("5.05")) {
		t.Fatalf("first allocation qty %s pnl %s", allocQty, allocPnL)
	}
}

func TestTinyProductIsStored(t *testing.T) {
	f := newFixture(t)
	f.buy("0.0001", "0.0001", "2026-03-01", "")
	pos := f.position()
	if !pos.TotalQty.Equal(d("0.0001")) || !pos.TotalCost.Equal(d("0.00000001")) {
		t.Fatalf("qty %s cost %s", pos.TotalQty, pos.TotalCost)
	}
}

func TestIdempotentBuyAndSell(t *testing.T) {
	f := newFixture(t)
	first := f.buy("100", "100", "2026-04-01", "buy-key-1")
	second := f.buy("100", "100", "2026-04-01", "buy-key-1")
	if first.ID != second.ID {
		t.Fatalf("replay id %s, first %s", second.ID, first.ID)
	}
	if f.countTx("BUY") != 1 {
		t.Fatalf("buy count %d", f.countTx("BUY"))
	}
	sold, err := f.sell("40", "150", "2026-04-02", "sell-key-1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.sell("40", "150", "2026-04-02", "sell-key-1")
	if err != nil {
		t.Fatal(err)
	}
	if sold.ID != again.ID || f.countTx("SELL") != 1 {
		t.Fatalf("sell replay id %s count %d", again.ID, f.countTx("SELL"))
	}
	var allocs int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM sell_allocations sa JOIN transactions t ON t.id = sa.sell_transaction_id WHERE t.user_id = ?`, f.userID).Scan(&allocs); err != nil {
		t.Fatal(err)
	}
	if allocs != 1 {
		t.Fatalf("allocations %d", allocs)
	}
	if !f.position().TotalQty.Equal(d("60")) {
		t.Fatalf("qty %s", f.position().TotalQty)
	}
}

func TestIdempotencyConflictAndOtherUser(t *testing.T) {
	f := newFixture(t)
	f.buy("10", "20", "2026-05-01", "shared-key")
	_, err := f.svc.Buy(context.Background(), TradeRequest{
		UserID: f.userID, SpiceGradeID: f.gradeID,
		Quantity: d("10"), Price: d("21"), ClientTradeDate: "2026-05-01", IdempotencyKey: "shared-key",
	})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("err = %v", err)
	}
	if f.countTx("BUY") != 1 {
		t.Fatalf("conflict applied a second buy: %d", f.countTx("BUY"))
	}

	other := newFixture(t)
	other.buy("3", "30", "2026-05-01", "shared-key")
	if other.countTx("BUY") != 1 || f.countTx("BUY") != 1 {
		t.Fatal("same key must be allowed for a different user")
	}
}

func TestFailedTradeRollsBackAndRetryWorks(t *testing.T) {
	f := newFixture(t)
	ctx := withBeforeCommit(context.Background(), func() error {
		return errors.New("simulated crash before commit")
	})
	_, err := f.svc.Buy(ctx, TradeRequest{
		UserID: f.userID, SpiceGradeID: f.gradeID,
		Quantity: d("5"), Price: d("10"), ClientTradeDate: "2026-06-01", IdempotencyKey: "crash-key",
	})
	if err == nil || !strings.Contains(err.Error(), "simulated crash") {
		t.Fatalf("err = %v", err)
	}
	if f.countTx("BUY") != 0 {
		t.Fatal("rolled-back buy left a transaction")
	}
	var keys, lots, positions int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM idempotency_keys WHERE user_id = ?`, f.userID).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM buy_lots WHERE user_id = ?`, f.userID).Scan(&lots); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM positions WHERE user_id = ?`, f.userID).Scan(&positions); err != nil {
		t.Fatal(err)
	}
	if keys != 0 || lots != 0 || positions != 0 {
		t.Fatalf("rollback leftover keys %d lots %d positions %d", keys, lots, positions)
	}

	f.buy("5", "10", "2026-06-01", "crash-key")
	if !f.position().TotalQty.Equal(d("5")) {
		t.Fatalf("retry qty %s", f.position().TotalQty)
	}

	f.buy("5", "10", "2026-06-02", "")
	ctx = withBeforeCommit(context.Background(), func() error { return errors.New("simulated sell crash") })
	_, err = f.svc.Sell(ctx, TradeRequest{
		UserID: f.userID, SpiceGradeID: f.gradeID,
		Quantity: d("4"), Price: d("12"), ClientTradeDate: "2026-06-03", IdempotencyKey: "sell-crash",
	})
	if err == nil {
		t.Fatal("expected sell crash")
	}
	if f.countTx("SELL") != 0 || !f.position().TotalQty.Equal(d("10")) {
		t.Fatalf("sell rollback qty %s sells %d", f.position().TotalQty, f.countTx("SELL"))
	}
	var allocs int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM sell_allocations sa JOIN buy_lots b ON b.id = sa.buy_lot_id WHERE b.user_id = ?`, f.userID).Scan(&allocs); err != nil {
		t.Fatal(err)
	}
	if allocs != 0 {
		t.Fatalf("allocations left after rollback: %d", allocs)
	}
}

func TestMissingPositionRollsBackSell(t *testing.T) {
	f := newFixture(t)
	f.buy("8", "10", "2026-07-01", "")
	if _, err := f.db.Exec(`DELETE FROM positions WHERE user_id = ?`, f.userID); err != nil {
		t.Fatal(err)
	}
	_, err := f.sell("1", "12", "2026-07-02", "")
	if !errors.Is(err, ErrPositionMissing) && !errors.Is(err, ErrPositionMismatch) {
		t.Fatalf("err = %v", err)
	}
	if f.countTx("SELL") != 0 {
		t.Fatal("sell was recorded without a position")
	}
	var remaining decimal.Decimal
	if err := f.db.QueryRow(`SELECT remaining_qty FROM buy_lots WHERE user_id = ?`, f.userID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if !remaining.Equal(d("8")) {
		t.Fatalf("lot remaining %s", remaining)
	}
	// The deleted position is the inconsistency under test. Put it back so later
	// checks of the shared database are not tripped by this fixture.
	if _, err := f.db.Exec(`
		INSERT INTO positions (user_id, spice_grade_id, total_qty, total_cost, realized_pnl)
		VALUES (?, ?, 8, 80, 0)`, f.userID, f.gradeID); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentSellCannotOversell(t *testing.T) {
	f := newFixture(t)
	f.buy("100", "10", "2026-08-01", "")
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = f.sell("100", "15", "2026-08-02", "")
		}(i)
	}
	close(start)
	wg.Wait()
	success := 0
	for _, err := range errs {
		if err == nil {
			success++
			continue
		}
		if !errors.Is(err, ErrInsufficientInventory) {
			t.Fatalf("unexpected sell error: %v", err)
		}
	}
	if success != 1 || f.countTx("SELL") != 1 || !f.position().TotalQty.Equal(d("0")) {
		t.Fatalf("success %d sells %d qty %s errs %v", success, f.countTx("SELL"), f.position().TotalQty, errs)
	}
}

func TestConcurrentIdempotentBuyAppliesOnce(t *testing.T) {
	f := newFixture(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	ids := make([]string, 2)
	errs := make([]error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			txn, err := f.svc.Buy(context.Background(), TradeRequest{
				UserID: f.userID, SpiceGradeID: f.gradeID,
				Quantity: d("25"), Price: d("40"), ClientTradeDate: "2026-09-01", IdempotencyKey: "concurrent-buy",
			})
			errs[i] = err
			if err == nil {
				ids[i] = txn.ID
			}
		}(i)
	}
	close(start)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if ids[0] == "" || ids[0] != ids[1] || f.countTx("BUY") != 1 || !f.position().TotalQty.Equal(d("25")) {
		t.Fatalf("ids %v qty %s count %d", ids, f.position().TotalQty, f.countTx("BUY"))
	}
}

func TestReconcileReportsMismatchWithoutRepairing(t *testing.T) {
	f := newFixture(t)
	before, err := f.svc.ReconcileInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range before {
		if strings.HasPrefix(row.UserID, "acc_") || row.UserID == f.userID {
			t.Fatalf("seed or fixture discrepancy: %+v", row)
		}
	}
	f.buy("4", "5", "2026-10-01", "")
	if _, err := f.db.Exec(`UPDATE positions SET total_qty = total_qty + 1 WHERE user_id = ?`, f.userID); err != nil {
		t.Fatal(err)
	}
	found, err := f.svc.ReconcileInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, row := range found {
		if row.UserID == f.userID && row.Kind == DiscrepancyQuantity {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("missing quantity discrepancy: %+v", found)
	}
	var qty decimal.Decimal
	if err := f.db.QueryRow(`SELECT total_qty FROM positions WHERE user_id = ?`, f.userID).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	if !qty.Equal(d("5")) {
		t.Fatalf("reconcile rewrote the position to %s", qty)
	}
}

func TestDatabaseRejectsNegativeQuantityAndBadForeignKey(t *testing.T) {
	f := newFixture(t)
	f.buy("1", "1", "2026-11-01", "")
	if _, err := f.db.Exec(`UPDATE buy_lots SET remaining_qty = -1 WHERE user_id = ?`, f.userID); err == nil {
		t.Fatal("negative remaining quantity was accepted")
	}
	_, err := f.db.Exec(`INSERT INTO transactions (id, user_id, spice_grade_id, type, quantity, price, trade_date)
		VALUES (?, ?, ?, 'BUY', 1, 1, '2026-11-02')`, ksuid.New().String(), "missing-user-000000000000", f.gradeID)
	if err == nil {
		t.Fatal("transaction with an unknown user was accepted")
	}
}

func TestColumnTypesAndDailyMark(t *testing.T) {
	f := newFixture(t)
	var columnType string
	if err := f.db.QueryRow(`
		SELECT COLUMN_TYPE FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'positions' AND COLUMN_NAME = 'total_cost'`).Scan(&columnType); err != nil {
		t.Fatal(err)
	}
	if columnType != "decimal(38,8)" {
		t.Fatalf("total_cost type %s", columnType)
	}
	f.buy("0.1", "100.25", "2026-12-01", "")
	f.buy("0.2", "101.50", "2026-12-02", "")
	today := time.Now().Format("2006-01-02")
	if _, err := f.db.Exec(`
		INSERT INTO daily_price (id, product_id, grade_id, price, date, time)
		SELECT ?, p.id, ?, 150.75, ?, '10:00:00' FROM products p
		JOIN grade g ON g.product_id = p.id WHERE g.id = ?`,
		ksuid.New().String(), f.gradeID, today, f.gradeID); err != nil {
		t.Fatal(err)
	}
	pos := f.position()
	// cost = 0.1*100.25 + 0.2*101.50 = 10.025 + 20.30 = 30.325
	// unrealized = 150.75*0.3 - 30.325 = 45.225 - 30.325 = 14.9
	if !pos.TotalCost.Equal(d("30.325")) || !pos.UnrealizedPnL.Equal(d("14.9")) {
		t.Fatalf("cost %s unrealized %s avg %s", pos.TotalCost, pos.UnrealizedPnL, pos.AvgCost)
	}
}

var (
	dbOnce sync.Once
	dbVal  *sql.DB
	dbErr  error
	dbSkip bool
	dbDSN  string
)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := ensureDB(t)
	userID := ksuid.New().String()
	productID := ksuid.New().String()
	gradeID := ksuid.New().String()
	if _, err := db.Exec(`INSERT INTO accounts (id, name, user_type, email, password) VALUES (?, 'Test', 'merchant', ?, 'x')`, userID, userID+"@test.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO products (id, name, category, description, status) VALUES (?, 'Spice', 'spice', '', 'active')`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO grade (id, product_id, name, description, status) VALUES (?, ?, 'A', '', 'active')`, gradeID, productID); err != nil {
		t.Fatal(err)
	}
	logger := util.NewLogger("error")
	repo, err := NewMysqlRepository(dbDSN, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	return &fixture{t: t, db: db, svc: NewMarketService(repo, logger), userID: userID, gradeID: gradeID}
}

func ensureDB(t *testing.T) *sql.DB {
	t.Helper()
	dbOnce.Do(func() { dbVal, dbErr, dbSkip = openTestDB() })
	if dbSkip {
		t.Skip(dbErr.Error())
	}
	if dbErr != nil {
		t.Fatal(dbErr)
	}
	return dbVal
}

func openTestDB() (*sql.DB, error, bool) {
	dsn := os.Getenv("SPICE_TEST_DSN")
	admin := os.Getenv("SPICE_TEST_ADMIN_DSN")
	name := os.Getenv("SPICE_TEST_DB_NAME")
	if name == "" {
		name = "spice_ledger_financial_test"
	}
	if dsn == "" {
		user := getenv("SPICE_TEST_DB_USER", "root")
		pass := getenv("SPICE_TEST_DB_PASSWORD", "1234")
		host := getenv("SPICE_TEST_DB_HOST", "127.0.0.1")
		port := getenv("SPICE_TEST_DB_PORT", "3306")
		admin = fmt.Sprintf("%s:%s@tcp(%s:%s)/?parseTime=true&multiStatements=true", user, pass, host, port)
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true", user, pass, host, port, name)
	}
	if !strings.HasSuffix(name, "_test") && !strings.Contains(dsn, "_test") {
		return nil, fmt.Errorf("refusing to reset a database whose name does not end in _test"), false
	}
	server, err := sql.Open("mysql", adminDSN(admin, dsn))
	if err != nil {
		return nil, err, true
	}
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.PingContext(pingCtx); err != nil {
		server.Close()
		return nil, fmt.Errorf("mysql is not available (%s): %w", adminDSN(admin, dsn), err), true
	}
	if _, err := server.Exec("DROP DATABASE IF EXISTS " + name); err != nil {
		server.Close()
		return nil, err, false
	}
	if _, err := server.Exec("CREATE DATABASE " + name); err != nil {
		server.Close()
		return nil, err, false
	}
	server.Close()

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err, false
	}
	db.SetMaxOpenConns(20)
	goose.SetTableName("goose_migrations")
	if err := goose.SetDialect("mysql"); err != nil {
		db.Close()
		return nil, err, false
	}
	if err := goose.Up(db, migrationsDir()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err), false
	}
	dbDSN = dsn
	return db, nil, false
}

func adminDSN(admin, dsn string) string {
	if admin != "" {
		return admin
	}
	if i := strings.Index(dsn, ")/"); i >= 0 {
		return dsn[:i+2] + strings.TrimPrefix(dsn[i+2:], strings.SplitN(dsn[i+2:], "?", 2)[0])
	}
	return dsn
}

func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "migrations")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
