-- +goose Up
-- Safeguards added only after the existing seed and any production rows already satisfy them.
-- Foreign keys use RESTRICT (the default): an account or grade with inventory cannot be deleted
-- out from under the ledger. Nothing cascades into financial history.

ALTER TABLE buy_lots
  ADD CONSTRAINT chk_buy_lots_remaining_lte_original CHECK (remaining_qty <= original_qty),
  ADD CONSTRAINT uq_buy_lots_transaction UNIQUE (transaction_id),
  ADD CONSTRAINT fk_lot_user FOREIGN KEY (user_id) REFERENCES accounts (id),
  ADD CONSTRAINT fk_lot_grade FOREIGN KEY (spice_grade_id) REFERENCES grade (id);

ALTER TABLE sell_allocations
  ADD CONSTRAINT uq_alloc_sell_lot UNIQUE (sell_transaction_id, buy_lot_id);

ALTER TABLE transactions
  ADD CONSTRAINT fk_txn_user FOREIGN KEY (user_id) REFERENCES accounts (id),
  ADD CONSTRAINT fk_txn_grade FOREIGN KEY (spice_grade_id) REFERENCES grade (id);

ALTER TABLE positions
  ADD CONSTRAINT fk_pos_user FOREIGN KEY (user_id) REFERENCES accounts (id),
  ADD CONSTRAINT fk_pos_grade FOREIGN KEY (spice_grade_id) REFERENCES grade (id);

INSERT IGNORE INTO schema_migrations (version, name, description)
VALUES (8, 'inventory_constraints', 'FIFO foreign keys, one lot per buy, one allocation per sell lot, remaining <= original');

-- +goose Down
ALTER TABLE positions
  DROP FOREIGN KEY fk_pos_user,
  DROP FOREIGN KEY fk_pos_grade;

ALTER TABLE transactions
  DROP FOREIGN KEY fk_txn_user,
  DROP FOREIGN KEY fk_txn_grade;

ALTER TABLE sell_allocations
  DROP INDEX uq_alloc_sell_lot;

ALTER TABLE buy_lots
  DROP FOREIGN KEY fk_lot_user,
  DROP FOREIGN KEY fk_lot_grade,
  DROP INDEX uq_buy_lots_transaction,
  DROP CHECK chk_buy_lots_remaining_lte_original;
