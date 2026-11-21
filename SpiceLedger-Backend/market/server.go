package market

import (
	"context"
	"fmt"
	"net"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	pb "github.com/Asif-Faizal/SpiceLedger-Backend/market/pb"
	"github.com/Asif-Faizal/SpiceLedger-Backend/util"
)

type GrpcServer struct {
	marketService Service
	logger        util.Logger
	config        *util.Config
	pb.UnimplementedMarketServiceServer
}

func ListenGrpcServer(service Service, logger util.Logger, config *util.Config) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", config.MarketGrpcPort))
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
			util.UnaryServerInterceptor(logger),
			util.AuthInterceptor(config.JWTSecret, config.BasicAuthUser, config.BasicAuthPass),
		)),
	)

	server := &GrpcServer{
		marketService: service,
		logger:        logger,
		config:        config,
	}
	pb.RegisterMarketServiceServer(grpcServer, server)
	reflection.Register(grpcServer)
	util.RegisterHealth(grpcServer, "market")

	return util.RunGRPC(lis, grpcServer, logger, "market")
}

func (server *GrpcServer) GetMarketMetrics(ctx context.Context, req *pb.GetMarketMetricsRequest) (*pb.GetMarketMetricsResponse, error) {
	totalTx, totalVol, tops, err := server.marketService.GetMarketMetrics(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	vol, volF := protoPair(totalVol)
	pbTops := make([]*pb.GetMarketMetricsResponse_TopProduct, len(tops))
	for i, top := range tops {
		exact, legacy := protoPair(top.Volume)
		pbTops[i] = &pb.GetMarketMetricsResponse_TopProduct{
			ProductName:   top.ProductName,
			GradeName:     top.GradeName,
			Volume:        legacy,
			VolumeDecimal: exact,
		}
	}
	return &pb.GetMarketMetricsResponse{
		TotalTransactions:  totalTx,
		TotalVolume:        volF,
		TotalVolumeDecimal: vol,
		TopProducts:        pbTops,
	}, nil
}

func (server *GrpcServer) Buy(ctx context.Context, req *pb.BuyRequest) (*pb.BuyResponse, error) {
	trade, err := server.tradeRequest(ctx, req.UserId, req.SpiceGradeId, req.QuantityDecimal, req.Quantity, req.PriceDecimal, req.Price, req.TradeDate, req.IdempotencyKey)
	if err != nil {
		return nil, toStatus(err)
	}
	txn, err := server.marketService.Buy(ctx, trade)
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.BuyResponse{Transaction: toProtoTransaction(txn)}, nil
}

func (server *GrpcServer) Sell(ctx context.Context, req *pb.SellRequest) (*pb.SellResponse, error) {
	trade, err := server.tradeRequest(ctx, req.UserId, req.SpiceGradeId, req.QuantityDecimal, req.Quantity, req.PriceDecimal, req.Price, req.TradeDate, req.IdempotencyKey)
	if err != nil {
		return nil, toStatus(err)
	}
	txn, err := server.marketService.Sell(ctx, trade)
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.SellResponse{Transaction: toProtoTransaction(txn)}, nil
}

func (server *GrpcServer) GetGradePosition(ctx context.Context, req *pb.GetGradePositionRequest) (*pb.GetGradePositionResponse, error) {
	userID, err := ResolveActorID(ctx, req.UserId)
	if err != nil {
		return nil, toStatus(err)
	}
	pos, err := server.marketService.GetGradePosition(ctx, userID, req.SpiceGradeId)
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.GetGradePositionResponse{Position: toProtoPosition(pos)}, nil
}

func (server *GrpcServer) GetPositions(ctx context.Context, req *pb.GetPositionsRequest) (*pb.GetPositionsResponse, error) {
	userID, err := ResolveActorID(ctx, req.UserId)
	if err != nil {
		return nil, toStatus(err)
	}
	positions, err := server.marketService.GetPositions(ctx, userID)
	if err != nil {
		return nil, toStatus(err)
	}
	pbPositions := make([]*pb.PositionView, len(positions))
	for i, pos := range positions {
		pbPositions[i] = toProtoPosition(pos)
	}
	return &pb.GetPositionsResponse{Positions: pbPositions}, nil
}

func (server *GrpcServer) ListGradeTransactions(ctx context.Context, req *pb.ListGradeTransactionsRequest) (*pb.ListGradeTransactionsResponse, error) {
	userID, err := ResolveActorID(ctx, req.UserId)
	if err != nil {
		return nil, toStatus(err)
	}
	txns, err := server.marketService.ListGradeTransactions(ctx, userID, req.SpiceGradeId, uint(req.Skip), uint(req.Take), req.Sort, req.DateFrom, req.DateTo)
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.ListGradeTransactionsResponse{Transactions: toProtoTransactions(txns)}, nil
}

func (server *GrpcServer) ListTransactions(ctx context.Context, req *pb.ListTransactionsRequest) (*pb.ListTransactionsResponse, error) {
	userID := req.UserId
	listAll := false
	if accountID, _ := ctx.Value(util.AccountIDKey).(string); accountID != "" {
		if isAdmin, _ := ctx.Value(util.IsAdminKey).(bool); isAdmin && userID == "" {
			listAll = true
		} else if !isAdmin {
			userID = accountID
		}
	} else if userID == "" {
		if isAdmin, _ := ctx.Value(util.IsAdminKey).(bool); isAdmin {
			listAll = true
		} else {
			return nil, status.Error(codes.InvalidArgument, "user_id is required")
		}
	}

	var txns []*Transaction
	var err error
	if listAll {
		txns, err = server.marketService.ListAllTransactions(ctx, uint(req.Skip), uint(req.Take), req.SpiceGradeId, req.SpiceGradeIds, req.Sort, req.DateFrom, req.DateTo)
	} else {
		txns, err = server.marketService.ListTransactions(ctx, userID, uint(req.Skip), uint(req.Take), req.SpiceGradeId, req.SpiceGradeIds, req.Sort, req.DateFrom, req.DateTo)
	}
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.ListTransactionsResponse{Transactions: toProtoTransactions(txns)}, nil
}

func (server *GrpcServer) GetHoldings(ctx context.Context, req *pb.GetHoldingsRequest) (*pb.GetHoldingsResponse, error) {
	userID, err := ResolveActorID(ctx, req.GetUserId())
	if err != nil {
		return nil, toStatus(err)
	}
	rows, err := server.marketService.GetEnrichedHoldings(ctx, userID)
	if err != nil {
		return nil, toStatus(err)
	}
	holdings := make([]*pb.EnrichedHolding, len(rows))
	for i, row := range rows {
		qty, qtyF := protoPair(row.TotalQty)
		cost, costF := protoPair(row.TotalCost)
		realized, realizedF := protoPair(row.RealizedPnL)
		today, todayF := protoPair(row.TodayPrice)
		holdings[i] = &pb.EnrichedHolding{
			SpiceGradeId:       row.SpiceGradeID,
			ProductName:        row.ProductName,
			GradeName:          row.GradeName,
			Quantity:           qtyF,
			TotalCost:          costF,
			RealizedPnl:        realizedF,
			TodayPrice:         todayF,
			QuantityDecimal:    qty,
			TotalCostDecimal:   cost,
			RealizedPnlDecimal: realized,
			TodayPriceDecimal:  today,
		}
	}
	return &pb.GetHoldingsResponse{Holdings: holdings}, nil
}

func (server *GrpcServer) GetRealizedPnLHistory(ctx context.Context, req *pb.GetRealizedPnLHistoryRequest) (*pb.GetRealizedPnLHistoryResponse, error) {
	userID, err := ResolveActorID(ctx, req.GetUserId())
	if err != nil {
		return nil, toStatus(err)
	}
	rows, err := server.marketService.GetDailyRealizedPnLByUser(ctx, userID, uint(req.GetDays()))
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*pb.RealizedPnLRow, len(rows))
	for i, row := range rows {
		amount, amountF := protoPair(row.DailyRealizedPnL)
		out[i] = &pb.RealizedPnLRow{
			Date:          formatDay(row.Date),
			Amount:        amountF,
			AmountDecimal: amount,
			SpiceGradeId:  row.SpiceGradeID,
			ProductName:   row.ProductName,
			GradeName:     row.GradeName,
		}
	}
	return &pb.GetRealizedPnLHistoryResponse{Rows: out}, nil
}

func (server *GrpcServer) GetTradeActivity(ctx context.Context, req *pb.GetTradeActivityRequest) (*pb.GetTradeActivityResponse, error) {
	userID, err := ResolveActorID(ctx, req.GetUserId())
	if err != nil {
		return nil, toStatus(err)
	}
	rows, err := server.marketService.GetDailyActivityByUser(ctx, userID, uint(req.GetDays()))
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*pb.TradeActivityRow, len(rows))
	for i, row := range rows {
		qty, qtyF := protoPair(row.Quantity)
		out[i] = &pb.TradeActivityRow{
			Date:            formatDay(row.Date),
			Type:            row.Type,
			Quantity:        qtyF,
			QuantityDecimal: qty,
			Count:           uint32(row.Count),
			SpiceGradeId:    row.SpiceGradeID,
			ProductName:     row.ProductName,
			GradeName:       row.GradeName,
		}
	}
	return &pb.GetTradeActivityResponse{Rows: out}, nil
}

func (server *GrpcServer) GetTradeStats(ctx context.Context, req *pb.GetTradeStatsRequest) (*pb.GetTradeStatsResponse, error) {
	userID, err := ResolveActorID(ctx, req.GetUserId())
	if err != nil {
		return nil, toStatus(err)
	}
	stats, err := server.marketService.GetPeriodTradeStats(ctx, userID, uint(req.GetDays()))
	if err != nil {
		return nil, toStatus(err)
	}
	buy, buyF := protoPair(stats.BuyVolumeInPeriod)
	sell, sellF := protoPair(stats.SellVolumeInPeriod)
	return &pb.GetTradeStatsResponse{
		TradesInPeriod:     uint32(stats.TradesInPeriod),
		BuyVolumeInPeriod:  buyF,
		SellVolumeInPeriod: sellF,
		BuyVolumeDecimal:   buy,
		SellVolumeDecimal:  sell,
	}, nil
}

func (server *GrpcServer) GetPriceSnapshots(ctx context.Context, req *pb.GetPriceSnapshotsRequest) (*pb.GetPriceSnapshotsResponse, error) {
	userID, err := ResolveActorID(ctx, req.GetUserId())
	if err != nil {
		return nil, toStatus(err)
	}
	snapshots, err := server.marketService.GetPriceSnapshotsForHoldings(ctx, userID)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*pb.PriceSnapshot, len(snapshots))
	for i, snap := range snapshots {
		today, todayF := protoPair(snap.TodayPrice)
		prev, prevF := protoPair(snap.PreviousPrice)
		out[i] = &pb.PriceSnapshot{
			SpiceGradeId:         snap.SpiceGradeID,
			ProductName:          snap.ProductName,
			GradeName:            snap.GradeName,
			TodayPrice:           todayF,
			PreviousPrice:        prevF,
			TodayPriceDecimal:    today,
			PreviousPriceDecimal: prev,
		}
	}
	return &pb.GetPriceSnapshotsResponse{Snapshots: out}, nil
}

func (server *GrpcServer) ReconcileInventory(ctx context.Context, _ *pb.ReconcileInventoryRequest) (*pb.ReconcileInventoryResponse, error) {
	if isAdmin, _ := ctx.Value(util.IsAdminKey).(bool); !isAdmin {
		return nil, status.Error(codes.PermissionDenied, "admin access required")
	}
	rows, err := server.marketService.ReconcileInventory(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	out := make([]*pb.InventoryDiscrepancy, len(rows))
	for i, row := range rows {
		out[i] = &pb.InventoryDiscrepancy{
			UserId:       row.UserID,
			SpiceGradeId: row.SpiceGradeID,
			Kind:         row.Kind,
			PositionQty:  row.PositionQty.String(),
			LotQty:       row.LotQty.String(),
			PositionCost: row.PositionCost.String(),
			LotCost:      row.LotCost.String(),
			Detail:       row.Detail,
		}
	}
	return &pb.ReconcileInventoryResponse{Discrepancies: out}, nil
}

func toProtoTransactions(txns []*Transaction) []*pb.Transaction {
	out := make([]*pb.Transaction, len(txns))
	for i, txn := range txns {
		out[i] = toProtoTransaction(txn)
	}
	return out
}
