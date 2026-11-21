package market

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

func (r *MysqlRepository) GetMarketMetrics(ctx context.Context) (uint32, decimal.Decimal, []GradeVolume, error) {
	var totalTx uint32
	var totalVol decimal.Decimal
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(quantity), 0) FROM transactions").Scan(&totalTx, &totalVol)
	if err != nil {
		return 0, decimal.Zero, nil, err
	}

	query := `SELECT p.name as product_name, g.name as grade_name, SUM(t.quantity) as vol
	          FROM transactions t
	          JOIN grade g ON t.spice_grade_id = g.id
	          JOIN products p ON g.product_id = p.id
	          GROUP BY p.name, g.name
	          ORDER BY vol DESC
	          LIMIT 5`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return totalTx, totalVol, nil, err
	}
	defer rows.Close()

	var tops []GradeVolume
	for rows.Next() {
		var row GradeVolume
		if err := rows.Scan(&row.ProductName, &row.GradeName, &row.Volume); err != nil {
			return totalTx, totalVol, nil, err
		}
		tops = append(tops, row)
	}
	if err := rows.Err(); err != nil {
		return totalTx, totalVol, nil, err
	}
	return totalTx, totalVol, tops, nil
}

func (r *MysqlRepository) GetEnrichedHoldings(ctx context.Context, userID string) ([]EnrichedHoldingRow, error) {
	start := time.Now()
	query := `SELECT p.spice_grade_id, pr.name, g.name,
	                 p.total_qty, p.total_cost, p.realized_pnl,
	                 COALESCE(dp.price, 0)
	          FROM positions p
	          INNER JOIN grade g ON g.id = p.spice_grade_id
	          INNER JOIN products pr ON pr.id = g.product_id
	          LEFT JOIN daily_price dp ON dp.grade_id = p.spice_grade_id AND dp.date = CURDATE()
	          WHERE p.user_id = ? AND p.total_qty > 0
	          ORDER BY p.total_qty DESC, pr.name, g.name`
	rows, err := r.db.QueryContext(ctx, query, userID)
	r.logQuery("GetEnrichedHoldings", query, start, err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var holdings []EnrichedHoldingRow
	for rows.Next() {
		var row EnrichedHoldingRow
		if err := rows.Scan(&row.SpiceGradeID, &row.ProductName, &row.GradeName, &row.TotalQty, &row.TotalCost, &row.RealizedPnL, &row.TodayPrice); err != nil {
			return nil, err
		}
		holdings = append(holdings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return holdings, nil
}

func (r *MysqlRepository) GetDailyRealizedPnLByUser(ctx context.Context, userID string, days uint) ([]DailyRealizedPnLRow, error) {
	start := time.Now()
	query := `SELECT t.trade_date AS d,
	                 COALESCE(SUM(sa.realized_pnl), 0),
	                 t.spice_grade_id,
	                 COALESCE(p.name, ''),
	                 COALESCE(g.name, '')
	          FROM sell_allocations sa
	          INNER JOIN transactions t ON t.id = sa.sell_transaction_id
	          LEFT JOIN grade g ON g.id = t.spice_grade_id
	          LEFT JOIN products p ON p.id = g.product_id
	          WHERE t.user_id = ?
	            AND t.trade_date >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
	          GROUP BY t.trade_date, t.spice_grade_id, p.name, g.name
	          ORDER BY d ASC`
	rows, err := r.db.QueryContext(ctx, query, userID, days)
	r.logQuery("GetDailyRealizedPnLByUser", query, start, err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DailyRealizedPnLRow
	for rows.Next() {
		var row DailyRealizedPnLRow
		if err := rows.Scan(&row.Date, &row.DailyRealizedPnL, &row.SpiceGradeID, &row.ProductName, &row.GradeName); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *MysqlRepository) GetDailyActivityByUser(ctx context.Context, userID string, days uint) ([]DailyActivityRow, error) {
	start := time.Now()
	query := `SELECT t.trade_date, t.type, COALESCE(SUM(t.quantity), 0), COUNT(*),
	                 t.spice_grade_id, COALESCE(p.name, ''), COALESCE(g.name, '')
	          FROM transactions t
	          LEFT JOIN grade g ON g.id = t.spice_grade_id
	          LEFT JOIN products p ON p.id = g.product_id
	          WHERE t.user_id = ?
	            AND t.trade_date >= DATE_SUB(CURDATE(), INTERVAL ? DAY)
	          GROUP BY t.trade_date, t.type, t.spice_grade_id, p.name, g.name
	          ORDER BY t.trade_date ASC`
	rows, err := r.db.QueryContext(ctx, query, userID, days)
	r.logQuery("GetDailyActivityByUser", query, start, err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DailyActivityRow
	for rows.Next() {
		var row DailyActivityRow
		if err := rows.Scan(&row.Date, &row.Type, &row.Quantity, &row.Count, &row.SpiceGradeID, &row.ProductName, &row.GradeName); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *MysqlRepository) GetPeriodTradeStats(ctx context.Context, userID string, days uint) (*PeriodTradeStats, error) {
	start := time.Now()
	query := `SELECT COUNT(*),
	                 COALESCE(SUM(CASE WHEN type = 'BUY' THEN quantity ELSE 0 END), 0),
	                 COALESCE(SUM(CASE WHEN type = 'SELL' THEN quantity ELSE 0 END), 0)
	          FROM transactions
	          WHERE user_id = ?
	            AND trade_date >= DATE_SUB(CURDATE(), INTERVAL ? DAY)`
	stats := &PeriodTradeStats{}
	err := r.db.QueryRowContext(ctx, query, userID, days).Scan(&stats.TradesInPeriod, &stats.BuyVolumeInPeriod, &stats.SellVolumeInPeriod)
	r.logQuery("GetPeriodTradeStats", query, start, err)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func (r *MysqlRepository) GetPriceSnapshotsForHoldings(ctx context.Context, userID string) ([]PriceSnapshot, error) {
	start := time.Now()
	query := `SELECT p.spice_grade_id, pr.name, g.name,
	                 COALESCE(dp_today.price, 0),
	                 COALESCE(dp_yesterday.price, 0)
	          FROM positions p
	          INNER JOIN grade g ON g.id = p.spice_grade_id
	          INNER JOIN products pr ON pr.id = g.product_id
	          LEFT JOIN daily_price dp_today
	            ON dp_today.grade_id = p.spice_grade_id AND dp_today.date = CURDATE()
	          LEFT JOIN daily_price dp_yesterday
	            ON dp_yesterday.grade_id = p.spice_grade_id
	           AND dp_yesterday.date = DATE_SUB(CURDATE(), INTERVAL 1 DAY)
	          WHERE p.user_id = ? AND p.total_qty > 0
	          ORDER BY pr.name, g.name`
	rows, err := r.db.QueryContext(ctx, query, userID)
	r.logQuery("GetPriceSnapshotsForHoldings", query, start, err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []PriceSnapshot
	for rows.Next() {
		var snap PriceSnapshot
		if err := rows.Scan(&snap.SpiceGradeID, &snap.ProductName, &snap.GradeName, &snap.TodayPrice, &snap.PreviousPrice); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return snapshots, nil
}
