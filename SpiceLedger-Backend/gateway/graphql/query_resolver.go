package graphql

import (
	"context"
	"fmt"
	"time"

	"github.com/Asif-Faizal/SpiceLedger-Backend/control/pb"
	"github.com/Asif-Faizal/SpiceLedger-Backend/market/money"
	marketpb "github.com/Asif-Faizal/SpiceLedger-Backend/market/pb"
	"github.com/shopspring/decimal"
)

// Products is the resolver for the products field.
func (r *queryResolver) Products(ctx context.Context, date *string, search *string) ([]*ProductWithGradesAndPrice, error) {
	dateStr := ""
	if date != nil {
		dateStr = *date
	} else {
		dateStr = time.Now().Format("2006-01-02")
	}

	searchStr := ""
	if search != nil {
		searchStr = *search
	}

	resp, err := r.server.controlClient.GetProductsWithGradesAndPrices(ctx, &pb.GetProductsWithGradesAndPricesRequest{
		Date:   dateStr,
		Search: searchStr,
	})
	if err != nil {
		return nil, err
	}

	products := make([]*ProductWithGradesAndPrice, len(resp.Products))
	for i, p := range resp.Products {
		grades := make([]*GradeWithPrice, len(p.Grades))
		for j, g := range p.Grades {
			grades[j] = &GradeWithPrice{
				ID:          g.Id,
				ProductID:   g.ProductId,
				Name:        g.Name,
				Description: g.Description,
				Status:      g.Status,
				Price:       g.Price,
			}
		}

		products[i] = &ProductWithGradesAndPrice{
			ID:          p.Id,
			Name:        p.Name,
			Category:    p.Category,
			Description: p.Description,
			Status:      p.Status,
			Grades:      grades,
		}
	}
	return products, nil
}

// GetGradePosition is the resolver for the getGradePosition field.
func (r *queryResolver) GetGradePosition(ctx context.Context, spiceGradeID string) (*PositionView, error) {
	resp, err := r.server.marketClient.GetGradePosition(ctx, &marketpb.GetGradePositionRequest{
		SpiceGradeId: spiceGradeID,
	})
	if err != nil {
		return nil, err
	}
	return positionFromPB(resp.Position)
}

// GetPositions is the resolver for the getPositions field.
func (r *queryResolver) GetPositions(ctx context.Context) ([]*PositionView, error) {
	resp, err := r.server.marketClient.GetPositions(ctx, &marketpb.GetPositionsRequest{})
	if err != nil {
		return nil, err
	}
	positions := make([]*PositionView, len(resp.Positions))
	for i, p := range resp.Positions {
		view, err := positionFromPB(p)
		if err != nil {
			return nil, err
		}
		positions[i] = view
	}
	return positions, nil
}

// ListGradeTransactions is the resolver for the listGradeTransactions field.
func (r *queryResolver) ListGradeTransactions(ctx context.Context, spiceGradeID string, skip *int, take *int, sort *string, dateFrom *string, dateTo *string) ([]*Transaction, error) {
	var skip32, take32 uint32
	if skip != nil {
		skip32 = uint32(*skip)
	}
	if take != nil {
		take32 = uint32(*take)
	}
	sortVal, dateFromVal, dateToVal := "", "", ""
	if sort != nil {
		sortVal = *sort
	}
	if dateFrom != nil {
		dateFromVal = *dateFrom
	}
	if dateTo != nil {
		dateToVal = *dateTo
	}
	resp, err := r.server.marketClient.ListGradeTransactions(ctx, &marketpb.ListGradeTransactionsRequest{
		SpiceGradeId: spiceGradeID,
		Skip:         skip32,
		Take:         take32,
		Sort:         sortVal,
		DateFrom:     dateFromVal,
		DateTo:       dateToVal,
	})
	if err != nil {
		return nil, err
	}
	return transactionsFromPB(resp.Transactions)
}

// AdminDashboard is the resolver for the adminDashboard field.
func (r *queryResolver) AdminDashboard(ctx context.Context) (*AdminDashboard, error) {
	systemResp, err := r.server.controlClient.GetSystemMetrics(ctx, &pb.GetSystemMetricsRequest{})
	if err != nil {
		return nil, err
	}
	marketResp, err := r.server.marketClient.GetMarketMetrics(ctx, &marketpb.GetMarketMetricsRequest{})
	if err != nil {
		return nil, err
	}
	txns, err := r.server.marketClient.ListTransactions(ctx, &marketpb.ListTransactionsRequest{Take: 5})
	if err != nil {
		return nil, err
	}
	recentTransactions, err := transactionsFromPB(txns.Transactions)
	if err != nil {
		return nil, err
	}
	topProducts := make([]*TopProduct, len(marketResp.TopProducts))
	for i, p := range marketResp.TopProducts {
		volume, err := wireDecimal(p.GetVolumeDecimal(), p.GetVolume())
		if err != nil {
			return nil, err
		}
		topProducts[i] = &TopProduct{
			Name:   fmt.Sprintf("%s - %s", p.ProductName, p.GradeName),
			Volume: volume,
		}
	}
	totalVolume, err := wireDecimal(marketResp.GetTotalVolumeDecimal(), marketResp.GetTotalVolume())
	if err != nil {
		return nil, err
	}
	return &AdminDashboard{
		TotalUsers:         int(systemResp.TotalUsers),
		TotalProducts:      int(systemResp.TotalProducts),
		TotalTransactions:  int(marketResp.TotalTransactions),
		TotalVolume:        totalVolume,
		RecentTransactions: recentTransactions,
		TopProducts:        topProducts,
	}, nil
}

// MerchantDashboard is the resolver for the merchantDashboard field.
func (r *queryResolver) MerchantDashboard(ctx context.Context, days *int) (*MerchantDashboard, error) {
	windowDays := trendWindowDays(days)

	holdingsResp, err := r.server.marketClient.GetHoldings(ctx, &marketpb.GetHoldingsRequest{})
	if err != nil {
		return nil, err
	}
	statsResp, err := r.server.marketClient.GetTradeStats(ctx, &marketpb.GetTradeStatsRequest{Days: windowDays})
	if err != nil {
		return nil, err
	}
	pnlResp, err := r.server.marketClient.GetRealizedPnLHistory(ctx, &marketpb.GetRealizedPnLHistoryRequest{Days: windowDays})
	if err != nil {
		return nil, err
	}
	activityResp, err := r.server.marketClient.GetTradeActivity(ctx, &marketpb.GetTradeActivityRequest{Days: windowDays})
	if err != nil {
		return nil, err
	}
	snapshotsResp, err := r.server.marketClient.GetPriceSnapshots(ctx, &marketpb.GetPriceSnapshotsRequest{})
	if err != nil {
		return nil, err
	}
	txnsResp, err := r.server.marketClient.ListTransactions(ctx, &marketpb.ListTransactionsRequest{Take: 5})
	if err != nil {
		return nil, err
	}

	holdings := make([]*MerchantHolding, 0, len(holdingsResp.Holdings))
	totalPortfolioValue := decimal.Zero
	for _, row := range holdingsResp.Holdings {
		qty, err := wireDecimal(row.GetQuantityDecimal(), row.GetQuantity())
		if err != nil {
			return nil, err
		}
		cost, err := wireDecimal(row.GetTotalCostDecimal(), row.GetTotalCost())
		if err != nil {
			return nil, err
		}
		realized, err := wireDecimal(row.GetRealizedPnlDecimal(), row.GetRealizedPnl())
		if err != nil {
			return nil, err
		}
		today, err := wireDecimal(row.GetTodayPriceDecimal(), row.GetTodayPrice())
		if err != nil {
			return nil, err
		}
		h := &MerchantHolding{
			SpiceGradeID: row.SpiceGradeId,
			ProductName:  row.ProductName,
			GradeName:    row.GradeName,
			Quantity:     qty,
			CostBasis:    cost,
			RealizedPnL:  realized,
			TodayPrice:   today,
		}
		if qty.IsPositive() {
			h.AvgCost = money.AvgCost(cost, qty)
		}
		if today.IsPositive() {
			h.MarketValue = today.Mul(qty)
			// Mark to market from cost, not from the rounded average.
			h.UnrealizedPnL = money.UnrealizedPnL(today, qty, cost)
		} else {
			h.MarketValue = cost
		}
		if cost.IsPositive() {
			h.UnrealizedPnLPercent = money.Percent(h.UnrealizedPnL, cost)
		}
		totalPortfolioValue = totalPortfolioValue.Add(h.MarketValue)
		holdings = append(holdings, h)
	}
	if totalPortfolioValue.IsPositive() {
		for _, h := range holdings {
			h.WeightPercent = money.Percent(h.MarketValue, totalPortfolioValue)
		}
	}

	portfolioMix := make([]*PortfolioSlice, len(holdings))
	for i, h := range holdings {
		portfolioMix[i] = &PortfolioSlice{
			Label:    fmt.Sprintf("%s - %s", h.ProductName, h.GradeName),
			Value:    h.MarketValue,
			Quantity: h.Quantity,
		}
	}

	buyVolume, err := wireDecimal(statsResp.GetBuyVolumeDecimal(), statsResp.GetBuyVolumeInPeriod())
	if err != nil {
		return nil, err
	}
	sellVolume, err := wireDecimal(statsResp.GetSellVolumeDecimal(), statsResp.GetSellVolumeInPeriod())
	if err != nil {
		return nil, err
	}
	summary := &MerchantSummary{
		OpenPositions:      len(holdings),
		TradesInPeriod:     int(statsResp.TradesInPeriod),
		BuyVolumeInPeriod:  buyVolume,
		SellVolumeInPeriod: sellVolume,
	}
	for _, h := range holdings {
		summary.TotalCost = summary.TotalCost.Add(h.CostBasis)
		summary.TotalRealizedPnL = summary.TotalRealizedPnL.Add(h.RealizedPnL)
		summary.TotalUnrealizedPnL = summary.TotalUnrealizedPnL.Add(h.UnrealizedPnL)
		summary.PortfolioValue = summary.PortfolioValue.Add(h.MarketValue)
		summary.TotalQuantityKg = summary.TotalQuantityKg.Add(h.Quantity)
	}
	summary.NetPnL = summary.TotalRealizedPnL.Add(summary.TotalUnrealizedPnL)

	pnlByDate := make(map[string]decimal.Decimal, len(pnlResp.Rows))
	for _, row := range pnlResp.Rows {
		amount, err := wireDecimal(row.GetAmountDecimal(), row.GetAmount())
		if err != nil {
			return nil, err
		}
		pnlByDate[row.Date] = pnlByDate[row.Date].Add(amount)
	}
	pnlTrend := make([]*PnLPoint, 0, windowDays+1)
	cumulativePnL := decimal.Zero
	for i := int(windowDays); i >= 0; i-- {
		key := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		daily := pnlByDate[key]
		cumulativePnL = cumulativePnL.Add(daily)
		pnlTrend = append(pnlTrend, &PnLPoint{
			Date:                  key,
			DailyRealizedPnL:      daily,
			CumulativeRealizedPnL: cumulativePnL,
		})
	}

	activityByDate := make(map[string]*ActivityDay, len(activityResp.Rows))
	for _, row := range activityResp.Rows {
		qty, err := wireDecimal(row.GetQuantityDecimal(), row.GetQuantity())
		if err != nil {
			return nil, err
		}
		day, ok := activityByDate[row.Date]
		if !ok {
			day = &ActivityDay{Date: row.Date}
			activityByDate[row.Date] = day
		}
		switch row.Type {
		case "BUY":
			day.BuyQuantity = day.BuyQuantity.Add(qty)
			day.BuyCount += int(row.Count)
		case "SELL":
			day.SellQuantity = day.SellQuantity.Add(qty)
			day.SellCount += int(row.Count)
		}
	}
	activityTrend := make([]*ActivityDay, 0, windowDays+1)
	for i := int(windowDays); i >= 0; i-- {
		key := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		if day, ok := activityByDate[key]; ok {
			activityTrend = append(activityTrend, day)
		} else {
			activityTrend = append(activityTrend, &ActivityDay{Date: key})
		}
	}

	recentTransactions, err := transactionsFromPB(txnsResp.Transactions)
	if err != nil {
		return nil, err
	}

	var insights []*MerchantInsight
	if len(holdings) == 0 {
		insights = []*MerchantInsight{{
			Kind:     "IDLE",
			Title:    "Start your portfolio",
			Body:     "You have no open positions yet. Buy your first spice grade to see holdings and P&L here.",
			Severity: "info",
		}}
	} else {
		if statsResp.TradesInPeriod == 0 {
			insights = append(insights, &MerchantInsight{
				Kind:     "IDLE",
				Title:    "Quiet period",
				Body:     "No trades in the selected window. Your portfolio snapshot still reflects current holdings.",
				Severity: "info",
			})
		}
		var best, worst *MerchantHolding
		for _, h := range holdings {
			if !h.TodayPrice.IsPositive() {
				continue
			}
			if best == nil || h.UnrealizedPnLPercent.GreaterThan(best.UnrealizedPnLPercent) {
				best = h
			}
			if worst == nil || h.UnrealizedPnLPercent.LessThan(worst.UnrealizedPnLPercent) {
				worst = h
			}
		}
		if best != nil && best.UnrealizedPnLPercent.IsPositive() {
			id := best.SpiceGradeID
			insights = append(insights, &MerchantInsight{
				Kind:         "WINNER",
				Title:        "Top performer",
				Body:         fmt.Sprintf("%s - %s is up %s%% unrealized.", best.ProductName, best.GradeName, best.UnrealizedPnLPercent.StringFixed(1)),
				SpiceGradeID: &id,
				Severity:     "success",
			})
		}
		if worst != nil && worst.UnrealizedPnLPercent.IsNegative() && (best == nil || worst.SpiceGradeID != best.SpiceGradeID) {
			id := worst.SpiceGradeID
			insights = append(insights, &MerchantInsight{
				Kind:         "LOSER",
				Title:        "Under pressure",
				Body:         fmt.Sprintf("%s - %s is down %s%% vs cost basis.", worst.ProductName, worst.GradeName, worst.UnrealizedPnLPercent.Abs().StringFixed(1)),
				SpiceGradeID: &id,
				Severity:     "warning",
			})
		}
		for _, h := range holdings {
			if h.WeightPercent.GreaterThanOrEqual(decimal.NewFromInt(70)) {
				id := h.SpiceGradeID
				insights = append(insights, &MerchantInsight{
					Kind:         "CONCENTRATION",
					Title:        "Concentrated portfolio",
					Body:         fmt.Sprintf("%s%% of portfolio value is in %s - %s.", h.WeightPercent.StringFixed(0), h.ProductName, h.GradeName),
					SpiceGradeID: &id,
					Severity:     "warning",
				})
				break
			}
		}
		periodRealized := decimal.Zero
		for _, row := range pnlResp.Rows {
			amount, err := wireDecimal(row.GetAmountDecimal(), row.GetAmount())
			if err != nil {
				return nil, err
			}
			periodRealized = periodRealized.Add(amount)
		}
		if periodRealized.IsPositive() {
			insights = append(insights, &MerchantInsight{
				Kind:     "MILESTONE",
				Title:    "Profitable period",
				Body:     fmt.Sprintf("You locked in %s realized P&L in the selected window.", periodRealized.StringFixed(2)),
				Severity: "success",
			})
		}
	}

	movers := make([]*PriceMover, len(snapshotsResp.Snapshots))
	flat := decimal.RequireFromString("0.01")
	for i, snap := range snapshotsResp.Snapshots {
		today, err := wireDecimal(snap.GetTodayPriceDecimal(), snap.GetTodayPrice())
		if err != nil {
			return nil, err
		}
		prev, err := wireDecimal(snap.GetPreviousPriceDecimal(), snap.GetPreviousPrice())
		if err != nil {
			return nil, err
		}
		m := &PriceMover{
			SpiceGradeID:  snap.SpiceGradeId,
			ProductName:   snap.ProductName,
			GradeName:     snap.GradeName,
			TodayPrice:    today,
			PreviousPrice: prev,
			Direction:     "FLAT",
		}
		if prev.IsPositive() && today.IsPositive() {
			m.ChangePercent = money.Percent(today.Sub(prev), prev)
			if m.ChangePercent.GreaterThan(flat) {
				m.Direction = "UP"
			} else if m.ChangePercent.LessThan(flat.Neg()) {
				m.Direction = "DOWN"
			}
		}
		movers[i] = m
	}

	return &MerchantDashboard{
		Summary:            summary,
		Holdings:           holdings,
		PortfolioMix:       portfolioMix,
		PnlTrend:           pnlTrend,
		ActivityTrend:      activityTrend,
		RecentTransactions: recentTransactions,
		Insights:           insights,
		Movers:             movers,
	}, nil
}

// ListTransactions is the resolver for the listTransactions field.
func (r *queryResolver) ListTransactions(ctx context.Context, skip *int, take *int, spiceGradeID *string, productID *string, sort *string, dateFrom *string, dateTo *string) ([]*Transaction, error) {
	var skip32, take32 uint32
	if skip != nil {
		skip32 = uint32(*skip)
	}
	if take != nil {
		take32 = uint32(*take)
	}
	sortVal, dateFromVal, dateToVal := "", "", ""
	if sort != nil {
		sortVal = *sort
	}
	if dateFrom != nil {
		dateFromVal = *dateFrom
	}
	if dateTo != nil {
		dateToVal = *dateTo
	}
	req := &marketpb.ListTransactionsRequest{
		Skip:     skip32,
		Take:     take32,
		Sort:     sortVal,
		DateFrom: dateFromVal,
		DateTo:   dateToVal,
	}
	if spiceGradeID != nil && *spiceGradeID != "" {
		req.SpiceGradeId = *spiceGradeID
	} else if productID != nil && *productID != "" {
		productsResp, err := r.server.controlClient.GetProductsWithGradesAndPrices(ctx, &pb.GetProductsWithGradesAndPricesRequest{
			Date: time.Now().Format("2006-01-02"),
		})
		if err != nil {
			return nil, err
		}
		var gradeIDs []string
		found := false
		for _, p := range productsResp.Products {
			if p.Id != *productID {
				continue
			}
			found = true
			gradeIDs = make([]string, 0, len(p.Grades))
			for _, g := range p.Grades {
				gradeIDs = append(gradeIDs, g.Id)
			}
			break
		}
		if !found {
			return nil, fmt.Errorf("product not found: %s", *productID)
		}
		if len(gradeIDs) == 0 {
			return []*Transaction{}, nil
		}
		req.SpiceGradeIds = gradeIDs
	}
	resp, err := r.server.marketClient.ListTransactions(ctx, req)
	if err != nil {
		return nil, err
	}
	return transactionsFromPB(resp.Transactions)
}

func trendWindowDays(days *int) uint32 {
	windowDays := uint32(7)
	if days != nil && *days > 0 {
		windowDays = uint32(*days)
		if windowDays > 90 {
			windowDays = 90
		}
	}
	return windowDays
}

// MerchantPnlTrend is the resolver for the merchantPnlTrend field.
func (r *queryResolver) MerchantPnlTrend(ctx context.Context, days *int) (*MerchantPnlTrend, error) {
	windowDays := trendWindowDays(days)
	pnlResp, err := r.server.marketClient.GetRealizedPnLHistory(ctx, &marketpb.GetRealizedPnLHistoryRequest{Days: windowDays})
	if err != nil {
		return nil, err
	}

	type dayAgg struct {
		total    decimal.Decimal
		products map[string]*PnLProductDay
	}
	byDate := make(map[string]*dayAgg)
	for _, row := range pnlResp.Rows {
		amount, err := wireDecimal(row.GetAmountDecimal(), row.GetAmount())
		if err != nil {
			return nil, err
		}
		agg, ok := byDate[row.Date]
		if !ok {
			agg = &dayAgg{products: map[string]*PnLProductDay{}}
			byDate[row.Date] = agg
		}
		agg.total = agg.total.Add(amount)
		key := row.SpiceGradeId
		if key == "" {
			key = row.ProductName + "|" + row.GradeName
		}
		prod, ok := agg.products[key]
		if !ok {
			prod = &PnLProductDay{
				SpiceGradeID: row.SpiceGradeId,
				ProductName:  row.ProductName,
				GradeName:    row.GradeName,
			}
			agg.products[key] = prod
		}
		prod.RealizedPnL = prod.RealizedPnL.Add(amount)
	}

	points := make([]*PnLDayDetail, 0, windowDays+1)
	cumulative := decimal.Zero
	period := decimal.Zero
	for i := int(windowDays); i >= 0; i-- {
		key := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		agg := byDate[key]
		daily := decimal.Zero
		products := []*PnLProductDay{}
		if agg != nil {
			daily = agg.total
			products = make([]*PnLProductDay, 0, len(agg.products))
			for _, p := range agg.products {
				products = append(products, p)
			}
		}
		cumulative = cumulative.Add(daily)
		period = period.Add(daily)
		points = append(points, &PnLDayDetail{
			Date:                  key,
			DailyRealizedPnL:      daily,
			CumulativeRealizedPnL: cumulative,
			Products:              products,
		})
	}
	return &MerchantPnlTrend{
		Days:              int(windowDays),
		PeriodRealizedPnL: period,
		Points:            points,
	}, nil
}

// MerchantActivityTrend is the resolver for the merchantActivityTrend field.
func (r *queryResolver) MerchantActivityTrend(ctx context.Context, days *int) (*MerchantActivityTrend, error) {
	windowDays := trendWindowDays(days)
	activityResp, err := r.server.marketClient.GetTradeActivity(ctx, &marketpb.GetTradeActivityRequest{Days: windowDays})
	if err != nil {
		return nil, err
	}

	type dayAgg struct {
		buyQty, sellQty     decimal.Decimal
		buyCount, sellCount int
		products            map[string]*ActivityProductDay
	}
	byDate := make(map[string]*dayAgg)
	for _, row := range activityResp.Rows {
		qty, err := wireDecimal(row.GetQuantityDecimal(), row.GetQuantity())
		if err != nil {
			return nil, err
		}
		agg, ok := byDate[row.Date]
		if !ok {
			agg = &dayAgg{products: map[string]*ActivityProductDay{}}
			byDate[row.Date] = agg
		}
		key := row.SpiceGradeId
		if key == "" {
			key = row.ProductName + "|" + row.GradeName
		}
		prod, ok := agg.products[key]
		if !ok {
			prod = &ActivityProductDay{
				SpiceGradeID: row.SpiceGradeId,
				ProductName:  row.ProductName,
				GradeName:    row.GradeName,
			}
			agg.products[key] = prod
		}
		switch row.Type {
		case "BUY":
			agg.buyQty = agg.buyQty.Add(qty)
			agg.buyCount += int(row.Count)
			prod.BuyQuantity = prod.BuyQuantity.Add(qty)
			prod.BuyCount += int(row.Count)
		case "SELL":
			agg.sellQty = agg.sellQty.Add(qty)
			agg.sellCount += int(row.Count)
			prod.SellQuantity = prod.SellQuantity.Add(qty)
			prod.SellCount += int(row.Count)
		}
	}

	points := make([]*ActivityDayDetail, 0, windowDays+1)
	totalBuy := decimal.Zero
	totalSell := decimal.Zero
	var totalTrades int
	for i := int(windowDays); i >= 0; i-- {
		key := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		agg := byDate[key]
		point := &ActivityDayDetail{Date: key, Products: []*ActivityProductDay{}}
		if agg != nil {
			point.BuyQuantity = agg.buyQty
			point.SellQuantity = agg.sellQty
			point.BuyCount = agg.buyCount
			point.SellCount = agg.sellCount
			products := make([]*ActivityProductDay, 0, len(agg.products))
			for _, p := range agg.products {
				products = append(products, p)
			}
			point.Products = products
			totalBuy = totalBuy.Add(agg.buyQty)
			totalSell = totalSell.Add(agg.sellQty)
			totalTrades += agg.buyCount + agg.sellCount
		}
		points = append(points, point)
	}
	return &MerchantActivityTrend{
		Days:              int(windowDays),
		TotalBuyQuantity:  totalBuy,
		TotalSellQuantity: totalSell,
		TotalTrades:       totalTrades,
		Points:            points,
	}, nil
}
