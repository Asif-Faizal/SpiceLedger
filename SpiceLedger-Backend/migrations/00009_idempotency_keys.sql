-- +goose Up
-- One row per (user, client key). A second insert of the same pair waits on the
-- InnoDB unique-key lock until the first transaction commits or rolls back, so
-- concurrent retries cannot both execute the trade.
--
-- status is only committed as 'completed', together with the trade. A rollback
-- removes the claim, so a failed attempt can be retried with the same key.
-- expires_at is 24 hours after creation, using the database clock. A later claim
-- of an expired key deletes it and runs as a new operation.

CREATE TABLE IF NOT EXISTS idempotency_keys (
  id              CHAR(27)        NOT NULL,
  idempotency_key VARCHAR(64)     NOT NULL,
  user_id         CHAR(27)        NOT NULL,
  operation       ENUM('BUY','SELL') NOT NULL,
  request_hash    CHAR(64)        NOT NULL,
  status          ENUM('processing','completed') NOT NULL,
  transaction_id  CHAR(27)        NULL,
  created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at      DATETIME        NOT NULL,

  PRIMARY KEY (id),
  UNIQUE KEY uq_idem_user_key (user_id, idempotency_key),
  INDEX idx_idem_expires (expires_at),

  CONSTRAINT fk_idem_user FOREIGN KEY (user_id) REFERENCES accounts (id),
  CONSTRAINT fk_idem_txn  FOREIGN KEY (transaction_id) REFERENCES transactions (id)
) ENGINE=InnoDB;

INSERT IGNORE INTO schema_migrations (version, name, description)
VALUES (9, 'idempotency_keys', 'Per-user idempotency keys for BUY and SELL');

-- +goose Down
DROP TABLE IF EXISTS idempotency_keys;
