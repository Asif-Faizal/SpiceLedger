package market

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Asif-Faizal/SpiceLedger-Backend/market/money"
	"github.com/Asif-Faizal/SpiceLedger-Backend/util"
	"github.com/shopspring/decimal"
)

type Service interface {
	Buy(ctx context.Context, req TradeRequest) (*Transaction, error)
	Sell(ctx context.Context, req TradeRequest) (*Transaction, error)
	GetGradePosition(ctx context.Context, userID string, spiceGradeID string) (*PositionView, error)
	GetPositions(ctx context.Context, userID string) ([]*PositionView, error)
	ListGradeTransactions(ctx context.Context, userID, spiceGradeID string, skip, take uint, sort, dateFrom, dateTo string) ([]*Transaction, error)
	ListTransactions(ctx context.Context, userID string, skip, take uint, spiceGradeID string, spiceGradeIDs []string, sort, dateFrom, dateTo string) ([]*Transaction, error)
	ListAllTransactions(ctx context.Context, skip, take uint, spiceGradeID string, spiceGradeIDs []string, sort, dateFrom, dateTo string) ([]*Transaction, error)
	GetMarketMetrics(ctx context.Context) (uint32, decimal.Decimal, []GradeVolume, error)
	GetEnrichedHoldings(ctx context.Context, userID string) ([]EnrichedHoldingRow, error)
	GetDailyRealizedPnLByUser(ctx context.Context, userID string, days uint) ([]DailyRealizedPnLRow, error)
	GetDailyActivityByUser(ctx context.Context, userID string, days uint) ([]DailyActivityRow, error)
	GetPeriodTradeStats(ctx context.Context, userID string, days uint) (*PeriodTradeStats, error)
	GetPriceSnapshotsForHoldings(ctx context.Context, userID string) ([]PriceSnapshot, error)
	// ReconcileInventory reports positions that do not match open lots.
	// It does not repair them.
	ReconcileInventory(ctx context.Context) ([]Discrepancy, error)
}

type MarketService struct {
	repository Repository
	logger     util.Logger
}

func NewMarketService(repository Repository, logger util.Logger) Service {
	return &MarketService{repository: repository, logger: logger}
}

func (s *MarketService) ListAllTransactions(ctx context.Context, skip, take uint, spiceGradeID string, spiceGradeIDs []string, sort, dateFrom, dateTo string) ([]*Transaction, error) {
	if take == 0 || take > 100 {
		take = 100
	}
	return s.repository.ListAllTransactions(ctx, skip, take, spiceGradeID, spiceGradeIDs, sort, dateFrom, dateTo)
}

func (s *MarketService) GetMarketMetrics(ctx context.Context) (uint32, decimal.Decimal, []GradeVolume, error) {
	return s.repository.GetMarketMetrics(ctx)
}

func (s *MarketService) GetGradePosition(ctx context.Context, userID string, spiceGradeID string) (*PositionView, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	if spiceGradeID == "" {
		return nil, invalidf("spice_grade_id is required")
	}

	pos, err := s.repository.GetGradePosition(ctx, userID, spiceGradeID)
	if err == sql.ErrNoRows {
		return nil, errors.New("no position found for this user and grade")
	}
	if err != nil {
		return nil, err
	}
	view, err := s.positionView(ctx, pos)
	if err != nil {
		return nil, err
	}
	return view, nil
}

func (s *MarketService) GetPositions(ctx context.Context, userID string) ([]*PositionView, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	positions, err := s.repository.GetPositionsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	views := make([]*PositionView, 0, len(positions))
	for _, pos := range positions {
		view, err := s.positionView(ctx, pos)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *MarketService) positionView(ctx context.Context, pos *Position) (*PositionView, error) {
	view := &PositionView{
		UserID:       pos.UserID,
		SpiceGradeID: pos.SpiceGradeID,
		TotalQty:     pos.TotalQty,
		TotalCost:    pos.TotalCost,
		RealizedPnL:  pos.RealizedPnL,
		UpdatedAt:    pos.UpdatedAt,
	}
	if pos.TotalQty.IsPositive() {
		view.AvgCost = money.AvgCost(pos.TotalCost, pos.TotalQty)
	}
	todayPrice, priceErr := s.repository.GetDailyPrice(ctx, pos.SpiceGradeID, time.Now())
	if priceErr == nil {
		view.TodayPrice = todayPrice
		view.UnrealizedPnL = money.UnrealizedPnL(todayPrice, pos.TotalQty, pos.TotalCost)
		return view, nil
	}
	if errors.Is(priceErr, ErrNoPriceAvailable) {
		return view, nil
	}
	return nil, priceErr
}

func (s *MarketService) ListGradeTransactions(ctx context.Context, userID, spiceGradeID string, skip, take uint, sort, dateFrom, dateTo string) ([]*Transaction, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	if spiceGradeID == "" {
		return nil, invalidf("spice_grade_id is required")
	}
	if take == 0 || take > 100 {
		take = 100
	}
	return s.repository.ListGradeTransactionsByUser(ctx, userID, spiceGradeID, skip, take, sort, dateFrom, dateTo)
}

func (s *MarketService) ListTransactions(ctx context.Context, userID string, skip, take uint, spiceGradeID string, spiceGradeIDs []string, sort, dateFrom, dateTo string) ([]*Transaction, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	if take == 0 || take > 100 {
		take = 100
	}
	return s.repository.ListTransactionsByUser(ctx, userID, skip, take, spiceGradeID, spiceGradeIDs, sort, dateFrom, dateTo)
}

func (s *MarketService) GetEnrichedHoldings(ctx context.Context, userID string) ([]EnrichedHoldingRow, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	return s.repository.GetEnrichedHoldings(ctx, userID)
}

func (s *MarketService) GetDailyRealizedPnLByUser(ctx context.Context, userID string, days uint) ([]DailyRealizedPnLRow, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	return s.repository.GetDailyRealizedPnLByUser(ctx, userID, clampDays(days))
}

func (s *MarketService) GetDailyActivityByUser(ctx context.Context, userID string, days uint) ([]DailyActivityRow, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	return s.repository.GetDailyActivityByUser(ctx, userID, clampDays(days))
}

func (s *MarketService) GetPeriodTradeStats(ctx context.Context, userID string, days uint) (*PeriodTradeStats, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	return s.repository.GetPeriodTradeStats(ctx, userID, clampDays(days))
}

func (s *MarketService) GetPriceSnapshotsForHoldings(ctx context.Context, userID string) ([]PriceSnapshot, error) {
	if userID == "" {
		return nil, invalidf("user_id is required")
	}
	return s.repository.GetPriceSnapshotsForHoldings(ctx, userID)
}

func (s *MarketService) ReconcileInventory(ctx context.Context) ([]Discrepancy, error) {
	rows, err := s.repository.FindInventoryDiscrepancies(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		s.logger.Service().Error().
			Str("kind", row.Kind).
			Str("user_id", row.UserID).
			Str("spice_grade_id", row.SpiceGradeID).
			Str("position_qty", row.PositionQty.String()).
			Str("lot_qty", row.LotQty.String()).
			Str("position_cost", row.PositionCost.String()).
			Str("lot_cost", row.LotCost.String()).
			Str("detail", row.Detail).
			Msg("inventory discrepancy; position was not modified")
	}
	return rows, nil
}

func clampDays(days uint) uint {
	if days == 0 {
		return DefaultDashboardDays
	}
	if days > MaxDashboardDays {
		return MaxDashboardDays
	}
	return days
}
