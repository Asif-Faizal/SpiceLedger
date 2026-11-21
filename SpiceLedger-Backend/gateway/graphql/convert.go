package graphql

import (
	"context"
	"fmt"
	"strings"

	"github.com/Asif-Faizal/SpiceLedger-Backend/market/money"
	marketpb "github.com/Asif-Faizal/SpiceLedger-Backend/market/pb"
	"github.com/Asif-Faizal/SpiceLedger-Backend/util"
	"github.com/shopspring/decimal"
)

func idempotencyKeyFrom(ctx context.Context, explicit *string) string {
	if explicit != nil {
		if key := strings.TrimSpace(*explicit); key != "" {
			return key
		}
	}
	if v, ok := ctx.Value(util.IdempotencyKey).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func transactionFromPB(t *marketpb.Transaction) (*Transaction, error) {
	if t == nil {
		return nil, fmt.Errorf("missing transaction")
	}
	qty, err := money.FromWire(t.GetQuantityDecimal(), t.GetQuantity())
	if err != nil {
		return nil, err
	}
	price, err := money.FromWire(t.GetPriceDecimal(), t.GetPrice())
	if err != nil {
		return nil, err
	}
	return &Transaction{
		ID:           t.GetId(),
		UserID:       t.GetUserId(),
		SpiceGradeID: t.GetSpiceGradeId(),
		Type:         t.GetType(),
		Quantity:     qty,
		Price:        price,
		TradeDate:    t.GetTradeDate(),
		CreatedAt:    t.GetCreatedAt(),
	}, nil
}

func transactionsFromPB(rows []*marketpb.Transaction) ([]*Transaction, error) {
	out := make([]*Transaction, len(rows))
	for i, row := range rows {
		txn, err := transactionFromPB(row)
		if err != nil {
			return nil, err
		}
		out[i] = txn
	}
	return out, nil
}

func positionFromPB(p *marketpb.PositionView) (*PositionView, error) {
	if p == nil {
		return nil, fmt.Errorf("missing position")
	}
	qty, err := money.FromWire(p.GetTotalQtyDecimal(), p.GetTotalQty())
	if err != nil {
		return nil, err
	}
	cost, err := money.FromWire(p.GetTotalCostDecimal(), p.GetTotalCost())
	if err != nil {
		return nil, err
	}
	avg, err := money.FromWire(p.GetAvgCostDecimal(), p.GetAvgCost())
	if err != nil {
		return nil, err
	}
	today, err := money.FromWire(p.GetTodayPriceDecimal(), p.GetTodayPrice())
	if err != nil {
		return nil, err
	}
	realized, err := money.FromWire(p.GetRealizedPnlDecimal(), p.GetRealizedPnl())
	if err != nil {
		return nil, err
	}
	unrealized, err := money.FromWire(p.GetUnrealizedPnlDecimal(), p.GetUnrealizedPnl())
	if err != nil {
		return nil, err
	}
	return &PositionView{
		UserID:        p.GetUserId(),
		SpiceGradeID:  p.GetSpiceGradeId(),
		TotalQty:      qty,
		TotalCost:     cost,
		AvgCost:       avg,
		TodayPrice:    today,
		RealizedPnL:   realized,
		UnrealizedPnL: unrealized,
		UpdatedAt:     p.GetUpdatedAt(),
	}, nil
}

func wireDecimal(exact string, legacy float64) (decimal.Decimal, error) {
	return money.FromWire(exact, legacy)
}
