package market

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/Asif-Faizal/SpiceLedger-Backend/market/money"
	"github.com/shopspring/decimal"
)

// idempotencyTTL is how long a completed key replays the original trade.
// The database clock applies the interval. After expiry the same key is a new operation.
const idempotencyTTL = 24 * time.Hour

// maxTradeAttempts bounds retries of a deadlock or lock-wait. Each attempt is a
// new transaction after the previous one rolled back, so a retry cannot double-apply.
const maxTradeAttempts = 3

func normalizeTrade(req *TradeRequest) error {
	if req.UserID == "" {
		return invalidf("user_id is required")
	}
	if req.SpiceGradeID == "" {
		return invalidf("spice_grade_id is required")
	}
	qty, err := money.NormalizeQuantity(req.Quantity)
	if err != nil {
		return invalidf("%s", err.Error())
	}
	price, err := money.NormalizePrice(req.Price)
	if err != nil {
		return invalidf("%s", err.Error())
	}
	req.Quantity = qty
	req.Price = price
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if err := validateIdempotencyKey(req.IdempotencyKey); err != nil {
		return err
	}
	req.ClientTradeDate = strings.TrimSpace(req.ClientTradeDate)
	if req.ClientTradeDate != "" {
		if _, err := time.Parse("2006-01-02", req.ClientTradeDate); err != nil {
			return invalidf("trade_date must be YYYY-MM-DD")
		}
	}
	return nil
}

func validateIdempotencyKey(key string) error {
	if key == "" {
		return nil
	}
	if len(key) > 64 {
		return invalidf("idempotency key must be at most 64 characters")
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return invalidf("idempotency key contains invalid characters")
	}
	return nil
}

func resolveTradeDate(raw string) time.Time {
	if raw != "" {
		// Already validated. UTC keeps the calendar date the client sent.
		t, _ := time.Parse("2006-01-02", raw)
		return t
	}
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// tradePayloadHash is the canonical request fingerprint. Quantity and price are
// fixed at scale 4 so "100.25" and "100.2500" hash the same. The client date
// string is included as sent; an omitted date is an empty field, not time.Now().
func tradePayloadHash(operation, gradeID string, qty, price decimal.Decimal, tradeDate string) string {
	canonical := strings.Join([]string{
		operation,
		gradeID,
		qty.StringFixed(money.QtyScale),
		price.StringFixed(money.PriceScale),
		tradeDate,
	}, "\n")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
