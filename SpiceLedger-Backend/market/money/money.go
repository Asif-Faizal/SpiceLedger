// Package money is the single precision policy for SpiceLedger financial values.
//
// Quantities and unit prices are exact at 4 decimal places. Their products
// (allocation cost, realized P&L, position cost) are exact at 8 decimal places
// and are not rounded. Average cost and percentages are quotients, so they are
// rounded only at the reporting boundary using half away from zero.
//
// Database columns follow the same split: DECIMAL(18,4) for quantity and unit
// price, DECIMAL(38,8) for derived amounts. DECIMAL(38,8) can store the full
// product of two DECIMAL(18,4) values (up to 28 integer digits and 8 decimal
// places) with room to spare. MySQL FLOAT/DOUBLE is never used for these values.
package money

import (
	"fmt"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	// QtyScale is the maximum scale accepted for a traded quantity.
	QtyScale int32 = 4
	// PriceScale is the maximum scale accepted for a unit price.
	PriceScale int32 = 4
	// AmountScale is QtyScale + PriceScale. Quantity × price is exact here.
	AmountScale int32 = 8
	// ReportScale is the rounding boundary for non-terminating quotients such as average cost.
	ReportScale int32 = 8
	// PercentScale is the rounding boundary for displayed ratios. It is not used for inventory or P&L.
	PercentScale int32 = 4
)

// MaxAbsQuantity is the largest absolute value DECIMAL(18,4) can store.
var MaxAbsQuantity = decimal.RequireFromString("99999999999999.9999")

// MaxAbsPrice matches the unit-price column, which is also DECIMAL(18,4).
var MaxAbsPrice = MaxAbsQuantity

// MaxAbsAmount is the largest absolute value DECIMAL(38,8) can store.
var MaxAbsAmount = decimal.RequireFromString("999999999999999999999999999999.99999999")

// NormalizeQuantity accepts a value that is exact at QtyScale and rejects a finer scale.
// Trailing zeros (100.2500) are accepted. A genuine extra digit (100.25001) is rejected.
func NormalizeQuantity(d decimal.Decimal) (decimal.Decimal, error) {
	return normalize(d, QtyScale, MaxAbsQuantity, "quantity", true)
}

// NormalizePrice accepts a value that is exact at PriceScale.
func NormalizePrice(d decimal.Decimal) (decimal.Decimal, error) {
	return normalize(d, PriceScale, MaxAbsPrice, "price", true)
}

func normalize(d decimal.Decimal, scale int32, max decimal.Decimal, name string, positive bool) (decimal.Decimal, error) {
	if !d.Equal(d.Round(scale)) {
		return decimal.Decimal{}, fmt.Errorf("%s supports at most %d decimal places", name, scale)
	}
	d = d.Round(scale)
	if positive && d.Sign() <= 0 {
		return decimal.Decimal{}, fmt.Errorf("%s must be greater than zero", name)
	}
	if d.Abs().GreaterThan(max) {
		return decimal.Decimal{}, fmt.Errorf("%s exceeds the supported maximum", name)
	}
	return d, nil
}

// EnsureAmount reports whether a derived amount fits in the DECIMAL(38,8) column.
// It does not round. Callers keep the exact product.
func EnsureAmount(d decimal.Decimal) error {
	if d.Abs().GreaterThan(MaxAbsAmount) {
		return fmt.Errorf("amount exceeds the supported maximum")
	}
	// A product of two scale-4 values is scale 8 or less. Reject anything finer
	// so a programming error cannot persist a value the column would round.
	if !d.Equal(d.Round(AmountScale)) {
		return fmt.Errorf("amount exceeds %d decimal places", AmountScale)
	}
	return nil
}

// AvgCost is totalCost / totalQty, rounded half away from zero to ReportScale.
// This is a display figure. P&L must use UnrealizedPnL, which does not divide.
func AvgCost(totalCost, totalQty decimal.Decimal) decimal.Decimal {
	if !totalQty.IsPositive() {
		return decimal.Zero
	}
	return totalCost.Div(totalQty).Round(ReportScale)
}

// UnrealizedPnL is todayPrice × quantity − cost. The multiplication is exact
// when todayPrice and quantity sit on their input scales; the result is not rounded.
func UnrealizedPnL(todayPrice, quantity, cost decimal.Decimal) decimal.Decimal {
	if !quantity.IsPositive() {
		return decimal.Zero
	}
	return todayPrice.Mul(quantity).Sub(cost)
}

// Percent rounds (part / whole) × 100 half away from zero to PercentScale.
func Percent(part, whole decimal.Decimal) decimal.Decimal {
	if whole.IsZero() {
		return decimal.Zero
	}
	return part.Div(whole).Mul(decimal.NewFromInt(100)).Round(PercentScale)
}

// FromWire reads a decimal string when the caller sent one.
// A legacy IEEE-754 value is accepted only when the string is empty, and it is
// snapped to AmountScale at this boundary. Nothing inside the trading engine
// should call this; it exists for the gRPC compatibility fields.
func FromWire(exact string, legacy float64) (decimal.Decimal, error) {
	exact = strings.TrimSpace(exact)
	if exact != "" {
		d, err := decimal.NewFromString(exact)
		if err != nil {
			return decimal.Decimal{}, fmt.Errorf("invalid decimal %q", exact)
		}
		return d, nil
	}
	if math.IsNaN(legacy) || math.IsInf(legacy, 0) {
		return decimal.Decimal{}, fmt.Errorf("invalid legacy decimal")
	}
	return decimal.NewFromFloat(legacy).Round(AmountScale), nil
}

// ToProto returns the exact decimal string and a legacy float for old readers.
// The float must not be used for further arithmetic.
func ToProto(d decimal.Decimal) (exact string, legacy float64) {
	f, _ := d.Float64()
	return d.String(), f
}
