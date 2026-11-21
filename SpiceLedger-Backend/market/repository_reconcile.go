package market

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shopspring/decimal"
)

type lotKey struct {
	user  string
	grade string
}

type lotSum struct {
	qty  decimal.Decimal
	cost decimal.Decimal
}

// FindInventoryDiscrepancies compares aggregate positions with the open lots.
// It runs in a read-only snapshot and does not take row locks or write anything.
// Lot cost is summed in Go with the same decimal product the trade path uses,
// so a SQL multiplication scale cannot disagree with the stored position.
func (r *MysqlRepository) FindInventoryDiscrepancies(ctx context.Context) ([]Discrepancy, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	sums := map[lotKey]lotSum{}
	lotRows, err := tx.QueryContext(ctx, `
		SELECT user_id, spice_grade_id, remaining_qty, price
		FROM buy_lots
		WHERE remaining_qty > 0`)
	if err != nil {
		return nil, err
	}
	for lotRows.Next() {
		var userID, gradeID string
		var qty, price decimal.Decimal
		if err := lotRows.Scan(&userID, &gradeID, &qty, &price); err != nil {
			lotRows.Close()
			return nil, err
		}
		key := lotKey{userID, gradeID}
		sum := sums[key]
		sum.qty = sum.qty.Add(qty)
		sum.cost = sum.cost.Add(qty.Mul(price))
		sums[key] = sum
	}
	if err := lotRows.Err(); err != nil {
		lotRows.Close()
		return nil, err
	}
	lotRows.Close()

	var out []Discrepancy
	posRows, err := tx.QueryContext(ctx, `
		SELECT user_id, spice_grade_id, total_qty, total_cost FROM positions`)
	if err != nil {
		return nil, err
	}
	seen := map[lotKey]bool{}
	for posRows.Next() {
		var userID, gradeID string
		var qty, cost decimal.Decimal
		if err := posRows.Scan(&userID, &gradeID, &qty, &cost); err != nil {
			posRows.Close()
			return nil, err
		}
		key := lotKey{userID, gradeID}
		seen[key] = true
		sum := sums[key]
		if qty.IsNegative() {
			out = append(out, Discrepancy{
				UserID: userID, SpiceGradeID: gradeID, Kind: DiscrepancyNegative,
				PositionQty: qty, LotQty: sum.qty, PositionCost: cost, LotCost: sum.cost,
				Detail: "position quantity is negative",
			})
		}
		if !qty.Equal(sum.qty) {
			out = append(out, Discrepancy{
				UserID: userID, SpiceGradeID: gradeID, Kind: DiscrepancyQuantity,
				PositionQty: qty, LotQty: sum.qty, PositionCost: cost, LotCost: sum.cost,
				Detail: fmt.Sprintf("position qty %s, open lots %s", qty.String(), sum.qty.String()),
			})
		}
		if !cost.Equal(sum.cost) {
			out = append(out, Discrepancy{
				UserID: userID, SpiceGradeID: gradeID, Kind: DiscrepancyCost,
				PositionQty: qty, LotQty: sum.qty, PositionCost: cost, LotCost: sum.cost,
				Detail: fmt.Sprintf("position cost %s, open lots %s", cost.String(), sum.cost.String()),
			})
		}
	}
	if err := posRows.Err(); err != nil {
		posRows.Close()
		return nil, err
	}
	posRows.Close()

	for key, sum := range sums {
		if seen[key] || sum.qty.IsZero() {
			continue
		}
		out = append(out, Discrepancy{
			UserID: key.user, SpiceGradeID: key.grade, Kind: DiscrepancyMissingPosition,
			LotQty: sum.qty, LotCost: sum.cost,
			Detail: "open buy lots have no position row",
		})
	}

	negRows, err := tx.QueryContext(ctx, `
		SELECT id, user_id, spice_grade_id, original_qty, remaining_qty
		FROM buy_lots
		WHERE remaining_qty < 0 OR original_qty < 0 OR remaining_qty > original_qty`)
	if err != nil {
		return nil, err
	}
	for negRows.Next() {
		var id, userID, gradeID string
		var original, remaining decimal.Decimal
		if err := negRows.Scan(&id, &userID, &gradeID, &original, &remaining); err != nil {
			negRows.Close()
			return nil, err
		}
		out = append(out, Discrepancy{
			UserID: userID, SpiceGradeID: gradeID, Kind: DiscrepancyNegative,
			LotQty: remaining,
			Detail: fmt.Sprintf("lot %s original %s remaining %s", id, original.String(), remaining.String()),
		})
	}
	if err := negRows.Err(); err != nil {
		negRows.Close()
		return nil, err
	}
	negRows.Close()

	orphanRows, err := tx.QueryContext(ctx, `
		SELECT sa.id, IFNULL(t.user_id, ''), IFNULL(t.spice_grade_id, ''), sa.sell_transaction_id, sa.buy_lot_id
		FROM sell_allocations sa
		LEFT JOIN transactions t ON t.id = sa.sell_transaction_id
		LEFT JOIN buy_lots b ON b.id = sa.buy_lot_id
		WHERE t.id IS NULL OR b.id IS NULL OR t.type <> 'SELL'`)
	if err != nil {
		return nil, err
	}
	for orphanRows.Next() {
		var id, userID, gradeID, sellID, lotID string
		if err := orphanRows.Scan(&id, &userID, &gradeID, &sellID, &lotID); err != nil {
			orphanRows.Close()
			return nil, err
		}
		out = append(out, Discrepancy{
			UserID: userID, SpiceGradeID: gradeID, Kind: DiscrepancyOrphanAllocation,
			Detail: fmt.Sprintf("allocation %s sell %s lot %s", id, sellID, lotID),
		})
	}
	if err := orphanRows.Err(); err != nil {
		orphanRows.Close()
		return nil, err
	}
	orphanRows.Close()

	badTxn, err := tx.QueryContext(ctx, `
		SELECT b.id, b.user_id, b.spice_grade_id, b.transaction_id
		FROM buy_lots b
		LEFT JOIN transactions t ON t.id = b.transaction_id
		WHERE t.id IS NULL OR t.type <> 'BUY'`)
	if err != nil {
		return nil, err
	}
	for badTxn.Next() {
		var id, userID, gradeID, txnID string
		if err := badTxn.Scan(&id, &userID, &gradeID, &txnID); err != nil {
			badTxn.Close()
			return nil, err
		}
		out = append(out, Discrepancy{
			UserID: userID, SpiceGradeID: gradeID, Kind: DiscrepancyInvalidTransaction,
			Detail: fmt.Sprintf("lot %s transaction %s is not a BUY", id, txnID),
		})
	}
	if err := badTxn.Err(); err != nil {
		badTxn.Close()
		return nil, err
	}
	badTxn.Close()

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
