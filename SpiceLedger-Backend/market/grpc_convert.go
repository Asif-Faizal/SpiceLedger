package market

import (
	"context"
	"errors"
	"time"

	"github.com/Asif-Faizal/SpiceLedger-Backend/market/money"
	pb "github.com/Asif-Faizal/SpiceLedger-Backend/market/pb"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (server *GrpcServer) tradeRequest(ctx context.Context, userID, gradeID, qtyExact string, qtyLegacy float64, priceExact string, priceLegacy float64, tradeDate, idempotencyKey string) (TradeRequest, error) {
	actor, err := ResolveActorID(ctx, userID)
	if err != nil {
		return TradeRequest{}, err
	}
	qty, err := money.FromWire(qtyExact, qtyLegacy)
	if err != nil {
		return TradeRequest{}, invalidf("quantity: %s", err.Error())
	}
	price, err := money.FromWire(priceExact, priceLegacy)
	if err != nil {
		return TradeRequest{}, invalidf("price: %s", err.Error())
	}
	return TradeRequest{
		UserID:          actor,
		SpiceGradeID:    gradeID,
		Quantity:        qty,
		Price:           price,
		ClientTradeDate: tradeDate,
		IdempotencyKey:  IdempotencyKeyFromContext(ctx, idempotencyKey),
	}, nil
}

func toStatus(err error) error {
	if err == nil {
		return nil
	}
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		return status.Error(codes.InvalidArgument, ve.Error())
	case errors.Is(err, ErrInsufficientInventory), errors.Is(err, ErrIdempotencyConflict), errors.Is(err, ErrPositionMissing), errors.Is(err, ErrPositionMismatch), errors.Is(err, ErrPositionNotUpdated):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, ErrIdempotencyInProgress), errors.Is(err, ErrDatabaseLockTimeout):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, ErrUserNotFound), errors.Is(err, ErrGradeNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		if st, ok := status.FromError(err); ok && st.Code() != codes.Unknown {
			return err
		}
		return status.Error(codes.Internal, err.Error())
	}
}

func protoPair(d decimal.Decimal) (string, float64) {
	return money.ToProto(d)
}

func toProtoTransaction(t *Transaction) *pb.Transaction {
	qty, qtyF := protoPair(t.Quantity)
	price, priceF := protoPair(t.Price)
	created := ""
	if !t.CreatedAt.IsZero() {
		created = t.CreatedAt.Format("2006-01-02 15:04:05")
	}
	return &pb.Transaction{
		Id:              t.ID,
		UserId:          t.UserID,
		SpiceGradeId:    t.SpiceGradeID,
		Type:            t.Type,
		Quantity:        qtyF,
		Price:           priceF,
		QuantityDecimal: qty,
		PriceDecimal:    price,
		TradeDate:       t.TradeDate.Format("2006-01-02"),
		CreatedAt:       created,
	}
}

func toProtoPosition(pos *PositionView) *pb.PositionView {
	qty, qtyF := protoPair(pos.TotalQty)
	cost, costF := protoPair(pos.TotalCost)
	avg, avgF := protoPair(pos.AvgCost)
	today, todayF := protoPair(pos.TodayPrice)
	realized, realizedF := protoPair(pos.RealizedPnL)
	unrealized, unrealizedF := protoPair(pos.UnrealizedPnL)
	return &pb.PositionView{
		UserId:               pos.UserID,
		SpiceGradeId:         pos.SpiceGradeID,
		TotalQty:             qtyF,
		TotalCost:            costF,
		AvgCost:              avgF,
		TodayPrice:           todayF,
		RealizedPnl:          realizedF,
		UnrealizedPnl:        unrealizedF,
		TotalQtyDecimal:      qty,
		TotalCostDecimal:     cost,
		AvgCostDecimal:       avg,
		TodayPriceDecimal:    today,
		RealizedPnlDecimal:   realized,
		UnrealizedPnlDecimal: unrealized,
		UpdatedAt:            pos.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

func formatDay(t time.Time) string {
	return t.Format("2006-01-02")
}
