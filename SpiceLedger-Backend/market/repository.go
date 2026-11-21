package market

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Asif-Faizal/SpiceLedger-Backend/market/money"
	"github.com/Asif-Faizal/SpiceLedger-Backend/util"
	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
)

type Repository interface {
	Close()

	InsertTransaction(ctx context.Context, tx *Transaction) (string, error)
	GetTransactionByID(ctx context.Context, id string) (*Transaction, error)
	ListGradeTransactionsByUser(ctx context.Context, userID, spiceGradeID string, skip, take uint, sort, dateFrom, dateTo string) ([]*Transaction, error)
	ListTransactionsByUser(ctx context.Context, userID string, skip, take uint, spiceGradeID string, spiceGradeIDs []string, sort, dateFrom, dateTo string) ([]*Transaction, error)
	ListAllTransactions(ctx context.Context, skip, take uint, spiceGradeID string, spiceGradeIDs []string, sort, dateFrom, dateTo string) ([]*Transaction, error)

	InsertBuyLot(ctx context.Context, lot *BuyLot) (string, error)
	// GetOpenBuyLots returns lots with remaining_qty > 0 in FIFO order.
	// FOR UPDATE locks them in that order. The caller must already hold the
	// position row for the same user and grade, so two trades cannot lock lots
	// in opposite orders.
	GetOpenBuyLots(ctx context.Context, userID string, spiceGradeID string) ([]*BuyLot, error)
	DeductBuyLotQty(ctx context.Context, lotID string, deductQty decimal.Decimal) error

	InsertSellAllocation(ctx context.Context, alloc *SellAllocation) error

	// LockPosition locks the aggregate row. sql.ErrNoRows means the user has
	// never traded this grade. The gap lock still serializes the first insert.
	LockPosition(ctx context.Context, userID, spiceGradeID string) (*Position, error)
	// ApplyPositionDelta adds quantity, cost, and realized P&L. Negative quantity
	// is a sell and requires the row to exist. RowsAffected must be exactly 1.
	ApplyPositionDelta(ctx context.Context, delta *Position) error
	GetGradePosition(ctx context.Context, userID string, spiceGradeID string) (*Position, error)
	GetPositionsByUser(ctx context.Context, userID string) ([]*Position, error)

	EnsureUser(ctx context.Context, userID string) error
	// EnsureGrade reports ErrGradeNotFound when the grade is missing.
	// forBuy rejects an inactive grade so new inventory cannot be opened,
	// while a sell of an inactive grade is still allowed so the merchant can exit.
	EnsureGrade(ctx context.Context, gradeID string, forBuy bool) error

	ClaimIdempotency(ctx context.Context, claim IdempotencyClaim) (*IdempotencyRecord, error)
	CompleteIdempotency(ctx context.Context, userID, key, transactionID string) error

	GetDailyPrice(ctx context.Context, gradeID string, date time.Time) (decimal.Decimal, error)

	BeginTx(ctx context.Context) (context.Context, *sql.Tx, error)
	GetMarketMetrics(ctx context.Context) (uint32, decimal.Decimal, []GradeVolume, error)

	GetEnrichedHoldings(ctx context.Context, userID string) ([]EnrichedHoldingRow, error)
	GetDailyRealizedPnLByUser(ctx context.Context, userID string, days uint) ([]DailyRealizedPnLRow, error)
	GetDailyActivityByUser(ctx context.Context, userID string, days uint) ([]DailyActivityRow, error)
	GetPeriodTradeStats(ctx context.Context, userID string, days uint) (*PeriodTradeStats, error)
	GetPriceSnapshotsForHoldings(ctx context.Context, userID string) ([]PriceSnapshot, error)

	// FindInventoryDiscrepancies is read-only. It does not lock or update positions.
	FindInventoryDiscrepancies(ctx context.Context) ([]Discrepancy, error)
}

type IdempotencyClaim struct {
	Key         string
	UserID      string
	Operation   string
	RequestHash string
}

type IdempotencyRecord struct {
	ID            string
	Key           string
	UserID        string
	Operation     string
	RequestHash   string
	Status        string
	TransactionID string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

type MysqlRepository struct {
	db     *sql.DB
	logger util.Logger
}

func NewMysqlRepository(url string, logger util.Logger) (Repository, error) {
	db, err := sql.Open("mysql", url)
	if err != nil {
		logger.Database().Fatal().Err(err).Msg("Database connection failed")
		return nil, err
	}
	if err = db.Ping(); err != nil {
		logger.Database().Fatal().Err(err).Msg("Database connection failed")
		return nil, err
	}
	logger.Database().Info().Msg("Database connection established")
	return &MysqlRepository{db: db, logger: logger}, nil
}

func (r *MysqlRepository) Close() {
	r.db.Close()
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type txKey struct{}

func (r *MysqlRepository) dbFromContext(ctx context.Context) execer {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok && tx != nil {
		return tx
	}
	return r.db
}

func (r *MysqlRepository) requireTx(ctx context.Context) error {
	tx, ok := ctx.Value(txKey{}).(*sql.Tx)
	if !ok || tx == nil {
		return errors.New("financial write requires a database transaction")
	}
	return nil
}

func (r *MysqlRepository) BeginTx(ctx context.Context) (context.Context, *sql.Tx, error) {
	// Repeatable read is InnoDB's default. Stating it keeps the FIFO snapshot
	// from depending on the session's isolation level.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ctx, nil, err
	}
	return context.WithValue(ctx, txKey{}, tx), tx, nil
}

func (r *MysqlRepository) logQuery(name, query string, start time.Time, err error) {
	r.logger.Database().Debug().
		Str("query", query).
		Str("duration", time.Since(start).String()).
		Bool("success", err == nil).
		Msg(name)
}

func isDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func isDeadlock(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1213
}

func isLockWaitTimeout(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1205
}

func oneRow(res sql.Result, ifNone error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		if ifNone != nil {
			return ifNone
		}
		return fmt.Errorf("expected 1 row affected, got %d", n)
	}
	return nil
}

func (r *MysqlRepository) InsertTransaction(ctx context.Context, t *Transaction) (string, error) {
	if err := r.requireTx(ctx); err != nil {
		return "", err
	}
	start := time.Now()
	query := `INSERT INTO transactions (id, user_id, spice_grade_id, type, quantity, price, trade_date)
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := r.dbFromContext(ctx).ExecContext(ctx, query,
		t.ID, t.UserID, t.SpiceGradeID, t.Type, t.Quantity, t.Price,
		t.TradeDate.Format("2006-01-02"),
	)
	r.logQuery("InsertTransaction", query, start, err)
	if err != nil {
		return "", err
	}
	if err := oneRow(res, nil); err != nil {
		return "", err
	}
	return t.ID, nil
}

func (r *MysqlRepository) GetTransactionByID(ctx context.Context, id string) (*Transaction, error) {
	start := time.Now()
	query := `SELECT id, user_id, spice_grade_id, type, quantity, price, trade_date, created_at
	          FROM transactions WHERE id = ?`
	row := r.dbFromContext(ctx).QueryRowContext(ctx, query, id)
	t, err := scanTransaction(row)
	r.logQuery("GetTransactionByID", query, start, err)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *MysqlRepository) InsertBuyLot(ctx context.Context, lot *BuyLot) (string, error) {
	if err := r.requireTx(ctx); err != nil {
		return "", err
	}
	start := time.Now()
	query := `INSERT INTO buy_lots (id, transaction_id, user_id, spice_grade_id, original_qty, remaining_qty, price, trade_date)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := r.dbFromContext(ctx).ExecContext(ctx, query,
		lot.ID, lot.TransactionID, lot.UserID, lot.SpiceGradeID,
		lot.OriginalQty, lot.RemainingQty, lot.Price,
		lot.TradeDate.Format("2006-01-02"),
	)
	r.logQuery("InsertBuyLot", query, start, err)
	if err != nil {
		return "", err
	}
	if err := oneRow(res, nil); err != nil {
		return "", err
	}
	return lot.ID, nil
}

func (r *MysqlRepository) GetOpenBuyLots(ctx context.Context, userID string, spiceGradeID string) ([]*BuyLot, error) {
	if err := r.requireTx(ctx); err != nil {
		return nil, err
	}
	start := time.Now()
	// trade_date, id is the FIFO order and the row-lock order.
	query := `SELECT id, transaction_id, user_id, spice_grade_id, original_qty, remaining_qty, price, trade_date, created_at
	          FROM buy_lots
	          WHERE user_id = ? AND spice_grade_id = ? AND remaining_qty > 0
	          ORDER BY trade_date ASC, id ASC
	          FOR UPDATE`
	rows, err := r.dbFromContext(ctx).QueryContext(ctx, query, userID, spiceGradeID)
	r.logQuery("GetOpenBuyLots", query, start, err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lots []*BuyLot
	for rows.Next() {
		lot, err := scanBuyLot(rows)
		if err != nil {
			return nil, err
		}
		lots = append(lots, lot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lots, nil
}

func (r *MysqlRepository) DeductBuyLotQty(ctx context.Context, lotID string, deductQty decimal.Decimal) error {
	if err := r.requireTx(ctx); err != nil {
		return err
	}
	start := time.Now()
	query := `UPDATE buy_lots SET remaining_qty = remaining_qty - ? WHERE id = ? AND remaining_qty >= ?`
	res, err := r.dbFromContext(ctx).ExecContext(ctx, query, deductQty, lotID, deductQty)
	r.logQuery("DeductBuyLotQty", query, start, err)
	if err != nil {
		return err
	}
	return oneRow(res, ErrInsufficientLotQty)
}

func (r *MysqlRepository) InsertSellAllocation(ctx context.Context, alloc *SellAllocation) error {
	if err := r.requireTx(ctx); err != nil {
		return err
	}
	start := time.Now()
	query := `INSERT INTO sell_allocations (id, sell_transaction_id, buy_lot_id, quantity, buy_price, sell_price, realized_pnl)
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	res, err := r.dbFromContext(ctx).ExecContext(ctx, query,
		alloc.ID, alloc.SellTransactionID, alloc.BuyLotID,
		alloc.Quantity, alloc.BuyPrice, alloc.SellPrice, alloc.RealizedPnL,
	)
	r.logQuery("InsertSellAllocation", query, start, err)
	if err != nil {
		return err
	}
	return oneRow(res, nil)
}

func (r *MysqlRepository) LockPosition(ctx context.Context, userID, spiceGradeID string) (*Position, error) {
	if err := r.requireTx(ctx); err != nil {
		return nil, err
	}
	start := time.Now()
	query := `SELECT user_id, spice_grade_id, total_qty, total_cost, realized_pnl, updated_at
	          FROM positions WHERE user_id = ? AND spice_grade_id = ? FOR UPDATE`
	row := r.dbFromContext(ctx).QueryRowContext(ctx, query, userID, spiceGradeID)
	pos, err := scanPosition(row)
	r.logQuery("LockPosition", query, start, err)
	if err != nil {
		return nil, err
	}
	return pos, nil
}

func (r *MysqlRepository) ApplyPositionDelta(ctx context.Context, delta *Position) error {
	if err := r.requireTx(ctx); err != nil {
		return err
	}
	if err := money.EnsureAmount(delta.TotalCost); err != nil {
		return err
	}
	if err := money.EnsureAmount(delta.RealizedPnL); err != nil {
		return err
	}
	// A sell must not INSERT a row: the inserted values would be negative and
	// the CHECK constraints would reject them before any UPDATE could run.
	if delta.TotalQty.IsNegative() || delta.TotalCost.IsNegative() {
		return r.addPositionDelta(ctx, delta)
	}
	start := time.Now()
	query := `INSERT INTO positions (user_id, spice_grade_id, total_qty, total_cost, realized_pnl)
	          VALUES (?, ?, ?, ?, ?)`
	res, err := r.dbFromContext(ctx).ExecContext(ctx, query,
		delta.UserID, delta.SpiceGradeID, delta.TotalQty, delta.TotalCost, delta.RealizedPnL,
	)
	r.logQuery("ApplyPositionDelta", query, start, err)
	if err == nil {
		return oneRow(res, ErrPositionNotUpdated)
	}
	if !isDuplicateKey(err) {
		return err
	}
	return r.addPositionDelta(ctx, delta)
}

func (r *MysqlRepository) addPositionDelta(ctx context.Context, delta *Position) error {
	start := time.Now()
	query := `UPDATE positions
	          SET total_qty = total_qty + ?,
	              total_cost = total_cost + ?,
	              realized_pnl = realized_pnl + ?
	          WHERE user_id = ? AND spice_grade_id = ?`
	res, err := r.dbFromContext(ctx).ExecContext(ctx, query,
		delta.TotalQty, delta.TotalCost, delta.RealizedPnL,
		delta.UserID, delta.SpiceGradeID,
	)
	r.logQuery("addPositionDelta", query, start, err)
	if err != nil {
		return err
	}
	// 0 rows means the aggregate disappeared between the lock and the update.
	// Do not insert a replacement from a sell delta.
	return oneRow(res, ErrPositionMissing)
}

func (r *MysqlRepository) GetGradePosition(ctx context.Context, userID string, spiceGradeID string) (*Position, error) {
	start := time.Now()
	query := `SELECT user_id, spice_grade_id, total_qty, total_cost, realized_pnl, updated_at
	          FROM positions WHERE user_id = ? AND spice_grade_id = ?`
	row := r.dbFromContext(ctx).QueryRowContext(ctx, query, userID, spiceGradeID)
	pos, err := scanPosition(row)
	r.logQuery("GetPosition", query, start, err)
	if err != nil {
		return nil, err
	}
	return pos, nil
}

func (r *MysqlRepository) GetPositionsByUser(ctx context.Context, userID string) ([]*Position, error) {
	start := time.Now()
	query := `SELECT user_id, spice_grade_id, total_qty, total_cost, realized_pnl, updated_at
	          FROM positions WHERE user_id = ? AND (total_qty > 0 OR realized_pnl <> 0)`
	rows, err := r.dbFromContext(ctx).QueryContext(ctx, query, userID)
	r.logQuery("GetPositionsByUser", query, start, err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []*Position
	for rows.Next() {
		pos, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		positions = append(positions, pos)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return positions, nil
}

func (r *MysqlRepository) EnsureUser(ctx context.Context, userID string) error {
	var id string
	err := r.dbFromContext(ctx).QueryRowContext(ctx, `SELECT id FROM accounts WHERE id = ?`, userID).Scan(&id)
	if err == sql.ErrNoRows {
		return ErrUserNotFound
	}
	return err
}

func (r *MysqlRepository) EnsureGrade(ctx context.Context, gradeID string, forBuy bool) error {
	var status string
	err := r.dbFromContext(ctx).QueryRowContext(ctx, `SELECT status FROM grade WHERE id = ?`, gradeID).Scan(&status)
	if err == sql.ErrNoRows {
		return ErrGradeNotFound
	}
	if err != nil {
		return err
	}
	if forBuy && status != "active" {
		return invalidf("spice grade is not active")
	}
	return nil
}

func (r *MysqlRepository) GetDailyPrice(ctx context.Context, gradeID string, date time.Time) (decimal.Decimal, error) {
	start := time.Now()
	query := `SELECT price FROM daily_price WHERE grade_id = ? AND date = ? LIMIT 1`
	var price decimal.Decimal
	err := r.dbFromContext(ctx).QueryRowContext(ctx, query, gradeID, date.Format("2006-01-02")).Scan(&price)
	r.logQuery("GetDailyPrice", query, start, err)
	if err == sql.ErrNoRows {
		return decimal.Zero, ErrNoPriceAvailable
	}
	if err != nil {
		return decimal.Zero, err
	}
	return price, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTransaction(sc scanner) (*Transaction, error) {
	t := &Transaction{}
	if err := sc.Scan(&t.ID, &t.UserID, &t.SpiceGradeID, &t.Type, &t.Quantity, &t.Price, &t.TradeDate, &t.CreatedAt); err != nil {
		return nil, err
	}
	return t, nil
}

func scanBuyLot(sc scanner) (*BuyLot, error) {
	lot := &BuyLot{}
	if err := sc.Scan(&lot.ID, &lot.TransactionID, &lot.UserID, &lot.SpiceGradeID,
		&lot.OriginalQty, &lot.RemainingQty, &lot.Price, &lot.TradeDate, &lot.CreatedAt); err != nil {
		return nil, err
	}
	return lot, nil
}

func scanPosition(sc scanner) (*Position, error) {
	pos := &Position{}
	if err := sc.Scan(&pos.UserID, &pos.SpiceGradeID, &pos.TotalQty, &pos.TotalCost, &pos.RealizedPnL, &pos.UpdatedAt); err != nil {
		return nil, err
	}
	return pos, nil
}
