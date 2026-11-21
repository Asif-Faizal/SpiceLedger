package graphql

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"encoding/json"

	"github.com/99designs/gqlgen/graphql"
	"github.com/Asif-Faizal/SpiceLedger-Backend/market/money"
	"github.com/shopspring/decimal"
)

// MarshalDecimal writes an exact decimal as a JSON string.
func MarshalDecimal(d decimal.Decimal) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		_, _ = io.WriteString(w, strconv.Quote(d.String()))
	})
}

// UnmarshalDecimal accepts a decimal string or a JSON number.
// A JSON number has already been parsed as float64 by the HTTP decoder, so it is
// snapped to the amount scale here. Callers that need exact tenths should send a string.
func UnmarshalDecimal(v any) (decimal.Decimal, error) {
	switch t := v.(type) {
	case string:
		return decimal.NewFromString(strings.TrimSpace(t))
	case json.Number:
		return decimal.NewFromString(t.String())
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return decimal.Decimal{}, fmt.Errorf("decimal must be finite")
		}
		return decimal.NewFromFloat(t).Round(money.AmountScale), nil
	case float32:
		return decimal.NewFromFloat32(t).Round(money.AmountScale), nil
	case int:
		return decimal.NewFromInt(int64(t)), nil
	case int32:
		return decimal.NewFromInt(int64(t)), nil
	case int64:
		return decimal.NewFromInt(t), nil
	default:
		return decimal.Decimal{}, fmt.Errorf("decimal must be a string or number")
	}
}
