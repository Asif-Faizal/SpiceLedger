package market

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (r *MysqlRepository) ListAllTransactions(ctx context.Context, skip, take uint, spiceGradeID string, spiceGradeIDs []string, sort, dateFrom, dateTo string) ([]*Transaction, error) {
	return r.listTransactions(ctx, "", spiceGradeID, spiceGradeIDs, skip, take, sort, dateFrom, dateTo)
}

func (r *MysqlRepository) ListGradeTransactionsByUser(ctx context.Context, userID, spiceGradeID string, skip, take uint, sort, dateFrom, dateTo string) ([]*Transaction, error) {
	if spiceGradeID == "" {
		return nil, fmt.Errorf("spice_grade_id is required")
	}
	return r.listTransactions(ctx, userID, spiceGradeID, nil, skip, take, sort, dateFrom, dateTo)
}

func (r *MysqlRepository) ListTransactionsByUser(ctx context.Context, userID string, skip, take uint, spiceGradeID string, spiceGradeIDs []string, sort, dateFrom, dateTo string) ([]*Transaction, error) {
	return r.listTransactions(ctx, userID, spiceGradeID, spiceGradeIDs, skip, take, sort, dateFrom, dateTo)
}

func (r *MysqlRepository) listTransactions(ctx context.Context, userID, spiceGradeID string, spiceGradeIDs []string, skip, take uint, sort, dateFrom, dateTo string) ([]*Transaction, error) {
	start := time.Now()
	if take == 0 || take > 100 {
		take = 100
	}
	where := []string{"1=1"}
	args := []any{}
	if userID != "" {
		where = append(where, "user_id = ?")
		args = append(args, userID)
	}
	if spiceGradeID != "" {
		where = append(where, "spice_grade_id = ?")
		args = append(args, spiceGradeID)
	} else if len(spiceGradeIDs) > 0 {
		placeholders := make([]string, len(spiceGradeIDs))
		for i, id := range spiceGradeIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		where = append(where, fmt.Sprintf("spice_grade_id IN (%s)", strings.Join(placeholders, ",")))
	}
	if dateFrom != "" {
		if _, err := time.Parse("2006-01-02", dateFrom); err != nil {
			return nil, fmt.Errorf("invalid date_from %q: use YYYY-MM-DD", dateFrom)
		}
		where = append(where, "trade_date >= ?")
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		if _, err := time.Parse("2006-01-02", dateTo); err != nil {
			return nil, fmt.Errorf("invalid date_to %q: use YYYY-MM-DD", dateTo)
		}
		where = append(where, "trade_date <= ?")
		args = append(args, dateTo)
	}
	orderBy := "ORDER BY trade_date DESC, id DESC"
	switch strings.ToUpper(strings.TrimSpace(sort)) {
	case "ASC", "OLDEST", "OLDEST_FIRST":
		orderBy = "ORDER BY trade_date ASC, id ASC"
	}
	query := fmt.Sprintf(`SELECT id, user_id, spice_grade_id, type, quantity, price, trade_date, created_at
	          FROM transactions WHERE %s %s LIMIT ? OFFSET ?`, strings.Join(where, " AND "), orderBy)
	args = append(args, take, skip)

	rows, err := r.dbFromContext(ctx).QueryContext(ctx, query, args...)
	r.logQuery("listTransactions", query, start, err)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []*Transaction
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return txns, nil
}
