package money

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestAddTenths(t *testing.T) {
	sum := decimal.RequireFromString("0.1").Add(decimal.RequireFromString("0.2"))
	if !sum.Equal(decimal.RequireFromString("0.3")) {
		t.Fatalf("0.1 + 0.2 = %s, want 0.3", sum)
	}
}

func TestNormalizeRejectsExtraScale(t *testing.T) {
	if _, err := NormalizeQuantity(decimal.RequireFromString("0.00001")); err == nil {
		t.Fatal("expected quantity with 5 decimal places to be rejected")
	}
	if _, err := NormalizePrice(decimal.RequireFromString("100.25001")); err == nil {
		t.Fatal("expected price with a non-zero 5th decimal to be rejected")
	}
	q, err := NormalizeQuantity(decimal.RequireFromString("100.2500"))
	if err != nil {
		t.Fatal(err)
	}
	if !q.Equal(decimal.RequireFromString("100.25")) {
		t.Fatalf("got %s", q)
	}
}

func TestLegacyFloatSnapsToExactTenths(t *testing.T) {
	// The float64 literal 0.1 is not a decimal tenth. The wire boundary snaps it.
	d, err := FromWire("", 0.1)
	if err != nil {
		t.Fatal(err)
	}
	q, err := NormalizeQuantity(d)
	if err != nil {
		t.Fatal(err)
	}
	if !q.Equal(decimal.RequireFromString("0.1")) {
		t.Fatalf("legacy 0.1 normalized to %s", q)
	}
	exact, err := FromWire("0.10", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !exact.Equal(decimal.RequireFromString("0.1")) {
		t.Fatalf("string 0.10 = %s", exact)
	}
}

func TestProductKeepsScaleEight(t *testing.T) {
	cost := decimal.RequireFromString("0.0001").Mul(decimal.RequireFromString("0.0001"))
	if err := EnsureAmount(cost); err != nil {
		t.Fatal(err)
	}
	if !cost.Equal(decimal.RequireFromString("0.00000001")) {
		t.Fatalf("cost = %s", cost)
	}
}

func TestLargeQuantityTimesPrice(t *testing.T) {
	qty := decimal.RequireFromString("99999999.1234")
	price := decimal.RequireFromString("0.0001")
	cost := qty.Mul(price)
	if err := EnsureAmount(cost); err != nil {
		t.Fatal(err)
	}
	if !cost.Equal(decimal.RequireFromString("9999.99991234")) {
		t.Fatalf("cost = %s", cost)
	}
}

func TestUnrealizedDoesNotUseRoundedAverage(t *testing.T) {
	cost := decimal.RequireFromString("10.025")
	qty := decimal.RequireFromString("0.3")
	today := decimal.RequireFromString("150.75")
	avg := AvgCost(cost, qty)
	if !avg.Equal(decimal.RequireFromString("33.41666667")) {
		t.Fatalf("avg = %s", avg)
	}
	// (today - rounded avg) × qty is 35.199999999, not the exact mark-to-market.
	pnl := UnrealizedPnL(today, qty, cost)
	if !pnl.Equal(decimal.RequireFromString("35.2")) {
		t.Fatalf("unrealized = %s", pnl)
	}
}

func TestRejectsNonPositiveAndOverflow(t *testing.T) {
	if _, err := NormalizeQuantity(decimal.Zero); err == nil {
		t.Fatal("expected zero quantity to be rejected")
	}
	if _, err := NormalizePrice(MaxAbsPrice.Add(decimal.RequireFromString("0.0001"))); err == nil {
		t.Fatal("expected price overflow to be rejected")
	}
}
