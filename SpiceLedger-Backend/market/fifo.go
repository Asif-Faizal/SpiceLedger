package market

import "github.com/shopspring/decimal"

// FIFOAllocation is one slice of a sell matched to an open buy lot.
// Cost and RealizedPnL are exact products. They are not rounded.
type FIFOAllocation struct {
	BuyLotID    string
	Quantity    decimal.Decimal
	BuyPrice    decimal.Decimal
	Cost        decimal.Decimal
	RealizedPnL decimal.Decimal
}

// AllocateFIFO walks lots from oldest to newest and consumes sellQty.
// The caller must pass lots already ordered by trade_date, then id.
// Lots are not mutated. A shortfall returns ErrInsufficientInventory and no partial result.
func AllocateFIFO(lots []*BuyLot, sellQty, sellPrice decimal.Decimal) ([]FIFOAllocation, decimal.Decimal, decimal.Decimal, error) {
	remaining := sellQty
	totalCost := decimal.Zero
	totalPnL := decimal.Zero
	allocs := make([]FIFOAllocation, 0, len(lots))

	for _, lot := range lots {
		if remaining.IsZero() {
			break
		}
		if lot.RemainingQty.Sign() <= 0 {
			continue
		}
		consume := lot.RemainingQty
		if consume.GreaterThan(remaining) {
			consume = remaining
		}
		// Quantity and both prices are scale 4, so these products are exact at scale 8.
		cost := consume.Mul(lot.Price)
		pnl := sellPrice.Sub(lot.Price).Mul(consume)
		allocs = append(allocs, FIFOAllocation{
			BuyLotID:    lot.ID,
			Quantity:    consume,
			BuyPrice:    lot.Price,
			Cost:        cost,
			RealizedPnL: pnl,
		})
		totalCost = totalCost.Add(cost)
		totalPnL = totalPnL.Add(pnl)
		remaining = remaining.Sub(consume)
	}
	if remaining.Sign() > 0 {
		return nil, decimal.Zero, decimal.Zero, ErrInsufficientInventory
	}
	return allocs, totalCost, totalPnL, nil
}

func sumOpenLots(lots []*BuyLot) (qty, cost decimal.Decimal) {
	qty = decimal.Zero
	cost = decimal.Zero
	for _, lot := range lots {
		qty = qty.Add(lot.RemainingQty)
		cost = cost.Add(lot.RemainingQty.Mul(lot.Price))
	}
	return qty, cost
}

// inventoryConsistent reports whether the aggregate row matches the open lots.
// A nil position is consistent only when there is no open quantity. Cost is compared
// exactly: both sides are sums of scale-4 quantity × scale-4 price.
func inventoryConsistent(pos *Position, lots []*BuyLot) error {
	qty, cost := sumOpenLots(lots)
	if pos == nil {
		if qty.IsZero() {
			return nil
		}
		return ErrPositionMissing
	}
	if pos.TotalQty.Equal(qty) && pos.TotalCost.Equal(cost) {
		return nil
	}
	return ErrPositionMismatch
}
