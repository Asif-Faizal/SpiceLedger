package market

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	return decimal.RequireFromString(s)
}

func lot(id, qty, price string) *BuyLot {
	return &BuyLot{ID: id, RemainingQty: d(qty), Price: d(price)}
}

func TestAllocateFIFOCanonical(t *testing.T) {
	lots := []*BuyLot{
		lot("A", "100", "100"),
		lot("B", "50", "110"),
		lot("C", "80", "120"),
	}
	allocs, cost, pnl, err := AllocateFIFO(lots, d("120"), d("150"))
	if err != nil {
		t.Fatal(err)
	}
	if len(allocs) != 2 {
		t.Fatalf("allocations = %d, want 2", len(allocs))
	}
	if allocs[0].BuyLotID != "A" || !allocs[0].Quantity.Equal(d("100")) || !allocs[0].RealizedPnL.Equal(d("5000")) {
		t.Fatalf("lot A = %+v", allocs[0])
	}
	if allocs[1].BuyLotID != "B" || !allocs[1].Quantity.Equal(d("20")) || !allocs[1].RealizedPnL.Equal(d("800")) {
		t.Fatalf("lot B = %+v", allocs[1])
	}
	if !cost.Equal(d("12200")) || !pnl.Equal(d("5800")) {
		t.Fatalf("cost %s pnl %s", cost, pnl)
	}
	// The input lots are not mutated. Lot C is untouched and not allocated.
	if !lots[2].RemainingQty.Equal(d("80")) {
		t.Fatalf("lot C remaining %s", lots[2].RemainingQty)
	}
}

func TestAllocateFIFOFractional(t *testing.T) {
	lots := []*BuyLot{
		lot("A", "0.1", "100.25"),
		lot("B", "0.2", "101.50"),
	}
	allocs, cost, pnl, err := AllocateFIFO(lots, d("0.15"), d("150.75"))
	if err != nil {
		t.Fatal(err)
	}
	if len(allocs) != 2 {
		t.Fatalf("allocations = %d", len(allocs))
	}
	if !allocs[0].Quantity.Equal(d("0.1")) || !allocs[0].Cost.Equal(d("10.025")) || !allocs[0].RealizedPnL.Equal(d("5.05")) {
		t.Fatalf("first = %+v", allocs[0])
	}
	if !allocs[1].Quantity.Equal(d("0.05")) || !allocs[1].Cost.Equal(d("5.075")) || !allocs[1].RealizedPnL.Equal(d("2.4625")) {
		t.Fatalf("second = %+v", allocs[1])
	}
	if !cost.Equal(d("15.1")) || !pnl.Equal(d("7.5125")) {
		t.Fatalf("cost %s pnl %s", cost, pnl)
	}
}

func TestAllocateFIFOPartialLotsRemainExact(t *testing.T) {
	lots := []*BuyLot{
		lot("A", "0.0001", "0.0001"),
		lot("B", "1", "2"),
	}
	allocs, cost, pnl, err := AllocateFIFO(lots, d("0.0001"), d("0.0002"))
	if err != nil {
		t.Fatal(err)
	}
	if len(allocs) != 1 || !allocs[0].Cost.Equal(d("0.00000001")) || !allocs[0].RealizedPnL.Equal(d("0.00000001")) {
		t.Fatalf("alloc %+v cost %s pnl %s", allocs, cost, pnl)
	}
}

func TestAllocateFIFOInsufficient(t *testing.T) {
	_, _, _, err := AllocateFIFO([]*BuyLot{lot("A", "1", "10")}, d("1.0001"), d("12"))
	if err != ErrInsufficientInventory {
		t.Fatalf("err = %v", err)
	}
}

func TestPayloadHashIgnoresTrailingZeros(t *testing.T) {
	a := tradePayloadHash("BUY", "g", d("100.25"), d("10"), "")
	b := tradePayloadHash("BUY", "g", d("100.2500"), d("10.0000"), "")
	if a != b {
		t.Fatalf("hashes differ\n%s\n%s", a, b)
	}
	c := tradePayloadHash("BUY", "g", d("100.25"), d("10.0001"), "")
	if a == c {
		t.Fatal("different price hashed the same")
	}
	if tradePayloadHash("SELL", "g", d("100.25"), d("10"), "") == a {
		t.Fatal("operation is not part of the hash")
	}
}
