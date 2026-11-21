package market

import (
	"time"

	"github.com/shopspring/decimal"
)

// TradeRequest is one BUY or SELL. Quantity and Price are decimal values, not floats.
// ClientTradeDate is the raw YYYY-MM-DD from the client. Empty means the server
// assigns today's date. The idempotency hash uses this raw string, so a retry
// that also omits the date matches even though the resolved clock time differs.
type TradeRequest struct {
	UserID          string
	SpiceGradeID    string
	Quantity        decimal.Decimal
	Price           decimal.Decimal
	ClientTradeDate string
	IdempotencyKey  string
}

type Transaction struct {
	ID           string
	UserID       string
	SpiceGradeID string
	Type         string
	Quantity     decimal.Decimal
	Price        decimal.Decimal
	TradeDate    time.Time
	CreatedAt    time.Time
}

type BuyLot struct {
	ID            string
	TransactionID string
	UserID        string
	SpiceGradeID  string
	OriginalQty   decimal.Decimal
	RemainingQty  decimal.Decimal
	Price         decimal.Decimal
	TradeDate     time.Time
	CreatedAt     time.Time
}

type SellAllocation struct {
	ID                string
	SellTransactionID string
	BuyLotID          string
	Quantity          decimal.Decimal
	BuyPrice          decimal.Decimal
	SellPrice         decimal.Decimal
	RealizedPnL       decimal.Decimal
	CreatedAt         time.Time
}

// Position fields on the write path are deltas (a sell passes a negative quantity
// and cost). On the read path they are the stored aggregates.
type Position struct {
	UserID       string
	SpiceGradeID string
	TotalQty     decimal.Decimal
	TotalCost    decimal.Decimal
	RealizedPnL  decimal.Decimal
	UpdatedAt    time.Time
}

// PositionView extends Position with unrealised P&L computed at read time.
type PositionView struct {
	UserID        string
	SpiceGradeID  string
	TotalQty      decimal.Decimal
	TotalCost     decimal.Decimal
	AvgCost       decimal.Decimal
	TodayPrice    decimal.Decimal
	RealizedPnL   decimal.Decimal
	UnrealizedPnL decimal.Decimal
	UpdatedAt     time.Time
}

// GradeVolume is one grade's traded quantity, used by the admin dashboard.
type GradeVolume struct {
	ProductName string
	GradeName   string
	Volume      decimal.Decimal
}

// Discrepancy is one read-only reconciliation finding. The reconciler never
// rewrites positions from these rows.
type Discrepancy struct {
	UserID       string
	SpiceGradeID string
	Kind         string
	PositionQty  decimal.Decimal
	LotQty       decimal.Decimal
	PositionCost decimal.Decimal
	LotCost      decimal.Decimal
	Detail       string
}

const (
	DiscrepancyQuantity           = "quantity_mismatch"
	DiscrepancyCost               = "cost_mismatch"
	DiscrepancyNegative           = "negative_inventory"
	DiscrepancyMissingPosition    = "missing_position"
	DiscrepancyOrphanAllocation   = "orphaned_allocation"
	DiscrepancyInvalidTransaction = "invalid_transaction_reference"
)

// --- Merchant dashboard domain models ---

const (
	DefaultDashboardDays = 7
	MaxDashboardDays     = 90
)

type EnrichedHoldingRow struct {
	SpiceGradeID string
	ProductName  string
	GradeName    string
	TotalQty     decimal.Decimal
	TotalCost    decimal.Decimal
	RealizedPnL  decimal.Decimal
	TodayPrice   decimal.Decimal
}

type DailyRealizedPnLRow struct {
	Date             time.Time
	DailyRealizedPnL decimal.Decimal
	SpiceGradeID     string
	ProductName      string
	GradeName        string
}

type DailyActivityRow struct {
	Date         time.Time
	Type         string // BUY or SELL
	Quantity     decimal.Decimal
	Count        int
	SpiceGradeID string
	ProductName  string
	GradeName    string
}

// PeriodTradeStats aggregates buy/sell volume and trade count over a date window.
type PeriodTradeStats struct {
	TradesInPeriod     int
	BuyVolumeInPeriod  decimal.Decimal
	SellVolumeInPeriod decimal.Decimal
}

// PriceSnapshot holds today and previous daily_price for a held grade.
type PriceSnapshot struct {
	SpiceGradeID  string
	ProductName   string
	GradeName     string
	TodayPrice    decimal.Decimal
	PreviousPrice decimal.Decimal
}
