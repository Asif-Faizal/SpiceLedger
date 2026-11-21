package market

import "fmt"

// ValidationError is a client input problem. It is not retried.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func invalidf(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

const (
	idempotencyProcessing = "processing"
	idempotencyCompleted  = "completed"
)

// ErrInsufficientInventory means the open FIFO lots cannot cover the sell.
var ErrInsufficientInventory = errString("insufficient inventory: sell quantity exceeds available buy lots")

// ErrIdempotencyConflict means this user already used the key for a different payload.
var ErrIdempotencyConflict = errString("idempotency conflict: key reused with a different payload")

// ErrIdempotencyInProgress means the key is claimed and the original result is not ready.
var ErrIdempotencyInProgress = errString("idempotency key is already in progress")

// ErrDatabaseLockTimeout means a row lock was not acquired. The transaction was rolled back.
// A client that sent an idempotency key should retry with that same key.
var ErrDatabaseLockTimeout = errString("database lock timeout; retry the same request")

// ErrPositionMissing means a sell or a position update found no aggregate row.
var ErrPositionMissing = errString("position row missing for an inventory change")

// ErrPositionNotUpdated means a position write did not affect the expected row.
var ErrPositionNotUpdated = errString("position update did not affect exactly one row")

// ErrPositionMismatch means the aggregate position does not match the open buy lots.
// The trade is rolled back and the stored position is left unchanged.
var ErrPositionMismatch = errString("position does not match remaining FIFO lots")

// ErrUserNotFound means the account row does not exist.
var ErrUserNotFound = errString("user not found")

// ErrGradeNotFound means the spice grade row does not exist.
var ErrGradeNotFound = errString("spice grade not found")

// ErrInsufficientLotQty is the row-level guard inside DeductBuyLotQty.
var ErrInsufficientLotQty = errString("insufficient buy lot quantity: possible concurrent oversell")

// ErrNoPriceAvailable means daily_price has no row for that grade and date.
var ErrNoPriceAvailable = errString("no daily price available for this grade on the given date")

type errString string

func (e errString) Error() string { return string(e) }
