package graphql

import "github.com/shopspring/decimal"

type ProductWithGradesAndPrice struct {
	ID          string            `json:"id" validate:"required,uuid4"`
	Name        string            `json:"name" validate:"required,min=3,max=255"`
	Category    string            `json:"category" validate:"required,oneof=spice others"`
	Description string            `json:"description" validate:"omitempty,min=3,max=255"`
	Status      string            `json:"status" validate:"required,oneof=active inactive"`
	Grades      []*GradeWithPrice `json:"grades,omitempty"`
}

type GradeWithPrice struct {
	ID          string  `json:"id" validate:"required,uuid4"`
	ProductID   string  `json:"product_id" validate:"required,uuid4"`
	Name        string  `json:"name" validate:"required,min=3,max=255"`
	Price       float64 `json:"price" validate:"required"`
	Description string  `json:"description" validate:"omitempty,min=3,max=255"`
	Status      string  `json:"status" validate:"required,oneof=active inactive"`
}

type Transaction struct {
	ID           string          `json:"id"`
	UserID       string          `json:"user_id"`
	SpiceGradeID string          `json:"spice_grade_id"`
	Type         string          `json:"type"`
	Quantity     decimal.Decimal `json:"quantity"`
	Price        decimal.Decimal `json:"price"`
	TradeDate    string          `json:"trade_date"`
	CreatedAt    string          `json:"created_at"`
}

type PositionView struct {
	UserID        string          `json:"user_id"`
	SpiceGradeID  string          `json:"spice_grade_id"`
	TotalQty      decimal.Decimal `json:"total_qty"`
	TotalCost     decimal.Decimal `json:"total_cost"`
	AvgCost       decimal.Decimal `json:"avg_cost"`
	TodayPrice    decimal.Decimal `json:"today_price"`
	RealizedPnL   decimal.Decimal `json:"realized_pnl"`
	UnrealizedPnL decimal.Decimal `json:"unrealized_pnl"`
	UpdatedAt     string          `json:"updated_at"`
}
