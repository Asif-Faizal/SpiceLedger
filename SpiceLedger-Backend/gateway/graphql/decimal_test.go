package graphql

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
)

func TestUnmarshalDecimalStringIsExact(t *testing.T) {
	got, err := UnmarshalDecimal("0.10")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(decimal.RequireFromString("0.10")) {
		t.Fatalf("got %s", got)
	}
}

func TestUnmarshalDecimalJSONNumberIsExact(t *testing.T) {
	got, err := UnmarshalDecimal(json.Number("0.1"))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(decimal.RequireFromString("0.1")) {
		t.Fatalf("got %s", got)
	}
}

func TestUnmarshalDecimalFloatSnapsToAmountScale(t *testing.T) {
	got, err := UnmarshalDecimal(0.1)
	if err != nil {
		t.Fatal(err)
	}
	sum := got.Add(got).Add(got)
	if !sum.Equal(decimal.RequireFromString("0.3")) {
		t.Fatalf("0.1+0.1+0.1 = %s", sum)
	}
}

func TestMarshalDecimalQuotesString(t *testing.T) {
	var buf bytes.Buffer
	MarshalDecimal(decimal.RequireFromString("100.2500")).MarshalGQL(&buf)
	if buf.String() != `"100.25"` {
		t.Fatalf("got %s", buf.String())
	}
}
