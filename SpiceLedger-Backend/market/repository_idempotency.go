package market

import (
	"context"
	"fmt"

	"github.com/segmentio/ksuid"
)

func (r *MysqlRepository) ClaimIdempotency(ctx context.Context, claim IdempotencyClaim) (*IdempotencyRecord, error) {
	if err := r.requireTx(ctx); err != nil {
		return nil, err
	}
	// Inserting the unique (user, key) pair is the lock. A concurrent insert of
	// the same pair waits until this transaction commits or rolls back.
	const insertSQL = `INSERT INTO idempotency_keys
	          (id, idempotency_key, user_id, operation, request_hash, status, expires_at)
	          VALUES (?, ?, ?, ?, ?, 'processing', DATE_ADD(NOW(), INTERVAL ? HOUR))`
	hours := int(idempotencyTTL.Hours())
	_, err := r.dbFromContext(ctx).ExecContext(ctx, insertSQL,
		ksuid.New().String(), claim.Key, claim.UserID, claim.Operation, claim.RequestHash, hours,
	)
	if err == nil {
		return nil, nil
	}
	if !isDuplicateKey(err) {
		return nil, err
	}

	rec, expired, err := r.lockIdempotency(ctx, claim.UserID, claim.Key)
	if err != nil {
		return nil, err
	}
	if expired {
		if _, err := r.dbFromContext(ctx).ExecContext(ctx, `DELETE FROM idempotency_keys WHERE id = ?`, rec.ID); err != nil {
			return nil, err
		}
		_, err = r.dbFromContext(ctx).ExecContext(ctx, insertSQL,
			ksuid.New().String(), claim.Key, claim.UserID, claim.Operation, claim.RequestHash, hours,
		)
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	if rec.Operation != claim.Operation || rec.RequestHash != claim.RequestHash {
		return nil, ErrIdempotencyConflict
	}
	switch rec.Status {
	case idempotencyCompleted:
		if rec.TransactionID == "" {
			return nil, fmt.Errorf("completed idempotency key %s has no transaction", claim.Key)
		}
		return rec, nil
	case idempotencyProcessing:
		// A committed 'processing' row has no live owner: this service commits
		// the claim only as 'completed'. Take the row over in this transaction.
		res, err := r.dbFromContext(ctx).ExecContext(ctx, `
			UPDATE idempotency_keys
			SET transaction_id = NULL,
			    expires_at = DATE_ADD(NOW(), INTERVAL ? HOUR),
			    created_at = NOW()
			WHERE id = ? AND status = 'processing'`, hours, rec.ID)
		if err != nil {
			return nil, err
		}
		if err := oneRow(res, ErrIdempotencyInProgress); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown idempotency status %q", rec.Status)
	}
}

func (r *MysqlRepository) lockIdempotency(ctx context.Context, userID, key string) (*IdempotencyRecord, bool, error) {
	query := `SELECT id, idempotency_key, user_id, operation, request_hash, status,
	                 IFNULL(transaction_id, ''), created_at, expires_at, expires_at <= NOW()
	          FROM idempotency_keys
	          WHERE user_id = ? AND idempotency_key = ?
	          FOR UPDATE`
	rec := &IdempotencyRecord{}
	var expired bool
	err := r.dbFromContext(ctx).QueryRowContext(ctx, query, userID, key).Scan(
		&rec.ID, &rec.Key, &rec.UserID, &rec.Operation, &rec.RequestHash, &rec.Status,
		&rec.TransactionID, &rec.CreatedAt, &rec.ExpiresAt, &expired,
	)
	if err != nil {
		return nil, false, err
	}
	return rec, expired, nil
}

func (r *MysqlRepository) CompleteIdempotency(ctx context.Context, userID, key, transactionID string) error {
	if err := r.requireTx(ctx); err != nil {
		return err
	}
	res, err := r.dbFromContext(ctx).ExecContext(ctx, `
		UPDATE idempotency_keys
		SET status = 'completed', transaction_id = ?
		WHERE user_id = ? AND idempotency_key = ? AND status = 'processing'`,
		transactionID, userID, key,
	)
	if err != nil {
		return err
	}
	return oneRow(res, ErrIdempotencyInProgress)
}
