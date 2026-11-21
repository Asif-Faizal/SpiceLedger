package market

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Asif-Faizal/SpiceLedger-Backend/market/money"
	"github.com/segmentio/ksuid"
	"github.com/shopspring/decimal"
)

type beforeCommitKey struct{}

// withBeforeCommit lets tests fail a trade after its writes and before commit.
// The hook is not set in production. A non-nil error rolls the whole transaction back,
// including the idempotency claim.
func withBeforeCommit(ctx context.Context, fn func() error) context.Context {
	return context.WithValue(ctx, beforeCommitKey{}, fn)
}

func runBeforeCommit(ctx context.Context) error {
	fn, _ := ctx.Value(beforeCommitKey{}).(func() error)
	if fn == nil {
		return nil
	}
	return fn()
}

type tradeSession struct {
	tx        *sql.Tx
	txCtx     context.Context
	committed bool
}

func (ts *tradeSession) close() {
	if ts != nil && !ts.committed && ts.tx != nil {
		_ = ts.tx.Rollback()
	}
}

func (s *MarketService) Buy(ctx context.Context, req TradeRequest) (*Transaction, error) {
	if err := normalizeTrade(&req); err != nil {
		return nil, err
	}
	return s.withRetry(ctx, "BUY", func() (*Transaction, error) {
		return s.buyOnce(ctx, req)
	})
}

func (s *MarketService) Sell(ctx context.Context, req TradeRequest) (*Transaction, error) {
	if err := normalizeTrade(&req); err != nil {
		return nil, err
	}
	return s.withRetry(ctx, "SELL", func() (*Transaction, error) {
		return s.sellOnce(ctx, req)
	})
}

func (s *MarketService) withRetry(ctx context.Context, op string, fn func() (*Transaction, error)) (*Transaction, error) {
	var last error
	for attempt := 1; attempt <= maxTradeAttempts; attempt++ {
		txn, err := fn()
		if err == nil {
			return txn, nil
		}
		last = err
		retry := isDeadlock(err) || isLockWaitTimeout(err) || errors.Is(err, ErrIdempotencyInProgress)
		if !retry || attempt == maxTradeAttempts {
			if isLockWaitTimeout(err) {
				return nil, ErrDatabaseLockTimeout
			}
			return nil, err
		}
		s.logger.Service().Warn().Err(err).Str("op", op).Int("attempt", attempt).Msg("retrying trade after transient database error")
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * 20 * time.Millisecond):
		}
	}
	return nil, last
}

func (s *MarketService) buyOnce(ctx context.Context, req TradeRequest) (*Transaction, error) {
	session, replay, err := s.beginTrade(ctx, req, "BUY", true)
	defer session.close()
	if err != nil {
		return nil, err
	}
	if replay != nil {
		return replay, nil
	}

	tradeDate := resolveTradeDate(req.ClientTradeDate)
	cost := req.Quantity.Mul(req.Price)
	if err := money.EnsureAmount(cost); err != nil {
		return nil, invalidf("%s", err.Error())
	}

	txn := &Transaction{
		ID:           ksuid.New().String(),
		UserID:       req.UserID,
		SpiceGradeID: req.SpiceGradeID,
		Type:         "BUY",
		Quantity:     req.Quantity,
		Price:        req.Price,
		TradeDate:    tradeDate,
	}
	if _, err = s.repository.InsertTransaction(session.txCtx, txn); err != nil {
		return nil, err
	}
	lot := &BuyLot{
		ID:            ksuid.New().String(),
		TransactionID: txn.ID,
		UserID:        req.UserID,
		SpiceGradeID:  req.SpiceGradeID,
		OriginalQty:   req.Quantity,
		RemainingQty:  req.Quantity,
		Price:         req.Price,
		TradeDate:     tradeDate,
	}
	if _, err = s.repository.InsertBuyLot(session.txCtx, lot); err != nil {
		return nil, err
	}
	if err = s.repository.ApplyPositionDelta(session.txCtx, &Position{
		UserID:       req.UserID,
		SpiceGradeID: req.SpiceGradeID,
		TotalQty:     req.Quantity,
		TotalCost:    cost,
		RealizedPnL:  decimal.Zero,
	}); err != nil {
		return nil, err
	}
	return s.finishTrade(ctx, session, req, txn)
}

func (s *MarketService) sellOnce(ctx context.Context, req TradeRequest) (*Transaction, error) {
	session, replay, err := s.beginTrade(ctx, req, "SELL", false)
	defer session.close()
	if err != nil {
		return nil, err
	}
	if replay != nil {
		return replay, nil
	}

	// beginTrade already locked these rows. Load them on the same transaction so
	// FIFO consumes the quantities that lock is protecting.
	lots, err := s.repository.GetOpenBuyLots(session.txCtx, req.UserID, req.SpiceGradeID)
	if err != nil {
		return nil, err
	}
	available, _ := sumOpenLots(lots)
	if available.LessThan(req.Quantity) {
		return nil, ErrInsufficientInventory
	}
	allocs, totalCost, totalPnL, err := AllocateFIFO(lots, req.Quantity, req.Price)
	if err != nil {
		return nil, err
	}
	if err := money.EnsureAmount(totalCost); err != nil {
		return nil, invalidf("%s", err.Error())
	}
	if err := money.EnsureAmount(totalPnL); err != nil {
		return nil, invalidf("%s", err.Error())
	}

	tradeDate := resolveTradeDate(req.ClientTradeDate)
	txn := &Transaction{
		ID:           ksuid.New().String(),
		UserID:       req.UserID,
		SpiceGradeID: req.SpiceGradeID,
		Type:         "SELL",
		Quantity:     req.Quantity,
		Price:        req.Price,
		TradeDate:    tradeDate,
	}
	if _, err = s.repository.InsertTransaction(session.txCtx, txn); err != nil {
		return nil, err
	}
	for _, alloc := range allocs {
		if err = s.repository.DeductBuyLotQty(session.txCtx, alloc.BuyLotID, alloc.Quantity); err != nil {
			return nil, err
		}
		if err = s.repository.InsertSellAllocation(session.txCtx, &SellAllocation{
			ID:                ksuid.New().String(),
			SellTransactionID: txn.ID,
			BuyLotID:          alloc.BuyLotID,
			Quantity:          alloc.Quantity,
			BuyPrice:          alloc.BuyPrice,
			SellPrice:         req.Price,
			RealizedPnL:       alloc.RealizedPnL,
		}); err != nil {
			return nil, err
		}
	}
	if err = s.repository.ApplyPositionDelta(session.txCtx, &Position{
		UserID:       req.UserID,
		SpiceGradeID: req.SpiceGradeID,
		TotalQty:     req.Quantity.Neg(),
		TotalCost:    totalCost.Neg(),
		RealizedPnL:  totalPnL,
	}); err != nil {
		return nil, err
	}
	return s.finishTrade(ctx, session, req, txn)
}

// beginTrade opens the transaction, validates the account and grade, claims the
// idempotency key, and locks the position before any lot lock.
// A non-nil replay means the key was already completed; the session is committed
// only in the sense that its locks were released, and the caller returns replay.
func (s *MarketService) beginTrade(ctx context.Context, req TradeRequest, operation string, forBuy bool) (*tradeSession, *Transaction, error) {
	txCtx, tx, err := s.repository.BeginTx(ctx)
	if err != nil {
		return nil, nil, err
	}
	session := &tradeSession{tx: tx, txCtx: txCtx}

	if err = s.repository.EnsureUser(txCtx, req.UserID); err != nil {
		return session, nil, err
	}
	if err = s.repository.EnsureGrade(txCtx, req.SpiceGradeID, forBuy); err != nil {
		return session, nil, err
	}

	if req.IdempotencyKey != "" {
		rec, claimErr := s.repository.ClaimIdempotency(txCtx, IdempotencyClaim{
			Key:         req.IdempotencyKey,
			UserID:      req.UserID,
			Operation:   operation,
			RequestHash: tradePayloadHash(operation, req.SpiceGradeID, req.Quantity, req.Price, req.ClientTradeDate),
		})
		if claimErr != nil {
			return session, nil, claimErr
		}
		if rec != nil {
			// Read the original trade after releasing this transaction. A
			// repeatable-read snapshot started before the original commit
			// would not be allowed to miss that row.
			if err = tx.Rollback(); err != nil {
				return session, nil, err
			}
			session.committed = true
			stored, err := s.repository.GetTransactionByID(ctx, rec.TransactionID)
			if err != nil {
				return session, nil, err
			}
			s.logger.Service().Info().
				Str("idempotency_key", req.IdempotencyKey).
				Str("transaction_id", stored.ID).
				Str("op", operation).
				Msg("idempotent replay")
			return session, stored, nil
		}
	}

	pos, err := s.repository.LockPosition(txCtx, req.UserID, req.SpiceGradeID)
	if err == sql.ErrNoRows {
		pos = nil
		err = nil
	}
	if err != nil {
		return session, nil, err
	}
	lots, err := s.repository.GetOpenBuyLots(txCtx, req.UserID, req.SpiceGradeID)
	if err != nil {
		return session, nil, err
	}
	if err = inventoryConsistent(pos, lots); err != nil {
		return session, nil, err
	}
	return session, nil, nil
}

func (s *MarketService) finishTrade(ctx context.Context, session *tradeSession, req TradeRequest, txn *Transaction) (*Transaction, error) {
	if req.IdempotencyKey != "" {
		if err := s.repository.CompleteIdempotency(session.txCtx, req.UserID, req.IdempotencyKey, txn.ID); err != nil {
			return nil, err
		}
	}
	// The position lock is still held. Re-read both sides and refuse to commit
	// a trade that would leave them apart.
	pos, err := s.repository.LockPosition(session.txCtx, req.UserID, req.SpiceGradeID)
	if err != nil {
		return nil, err
	}
	lots, err := s.repository.GetOpenBuyLots(session.txCtx, req.UserID, req.SpiceGradeID)
	if err != nil {
		return nil, err
	}
	if err = inventoryConsistent(pos, lots); err != nil {
		return nil, err
	}
	if err = runBeforeCommit(session.txCtx); err != nil {
		return nil, err
	}
	if err = session.tx.Commit(); err != nil {
		return nil, err
	}
	session.committed = true

	stored, err := s.repository.GetTransactionByID(ctx, txn.ID)
	if err != nil {
		// The trade is committed. Returning the in-memory row keeps a client
		// that has no idempotency key from treating a committed buy as a failure.
		s.logger.Service().Error().Err(err).Str("transaction_id", txn.ID).Msg("trade committed but reload failed")
		return txn, nil
	}
	return stored, nil
}
