-- +goose Up
-- Quantity and unit price stay at scale 4, with more integer digits than DECIMAL(15,4).
-- Derived amounts move to DECIMAL(38,8). A product of two DECIMAL(18,4) values is exact
-- at scale 8 and needs up to 28 integer digits; DECIMAL(38,8) holds that without rounding.
-- Existing DECIMAL(15,4) values are a subset and are preserved by these MODIFYs.

ALTER TABLE transactions
  MODIFY quantity DECIMAL(18,4) NOT NULL,
  MODIFY price    DECIMAL(18,4) NOT NULL;

ALTER TABLE buy_lots
  MODIFY original_qty  DECIMAL(18,4) NOT NULL,
  MODIFY remaining_qty DECIMAL(18,4) NOT NULL,
  MODIFY price         DECIMAL(18,4) NOT NULL;

ALTER TABLE sell_allocations
  MODIFY quantity     DECIMAL(18,4) NOT NULL,
  MODIFY buy_price    DECIMAL(18,4) NOT NULL,
  MODIFY sell_price   DECIMAL(18,4) NOT NULL,
  MODIFY realized_pnl DECIMAL(38,8) NOT NULL;

ALTER TABLE positions
  MODIFY total_qty    DECIMAL(18,4) NOT NULL DEFAULT 0,
  MODIFY total_cost   DECIMAL(38,8) NOT NULL DEFAULT 0,
  MODIFY realized_pnl DECIMAL(38,8) NOT NULL DEFAULT 0;

INSERT IGNORE INTO schema_migrations (version, name, description)
VALUES (7, 'financial_precision', 'Widen quantity/price to DECIMAL(18,4) and derived amounts to DECIMAL(38,8)');

-- +goose Down
-- Down is only safe while every stored amount still fits in the original DECIMAL(15,4) columns.
ALTER TABLE positions
  MODIFY total_qty    DECIMAL(15,4) NOT NULL DEFAULT 0,
  MODIFY total_cost   DECIMAL(15,4) NOT NULL DEFAULT 0,
  MODIFY realized_pnl DECIMAL(15,4) NOT NULL DEFAULT 0;

ALTER TABLE sell_allocations
  MODIFY quantity     DECIMAL(15,4) NOT NULL,
  MODIFY buy_price    DECIMAL(15,4) NOT NULL,
  MODIFY sell_price   DECIMAL(15,4) NOT NULL,
  MODIFY realized_pnl DECIMAL(15,4) NOT NULL;

ALTER TABLE buy_lots
  MODIFY original_qty  DECIMAL(15,4) NOT NULL,
  MODIFY remaining_qty DECIMAL(15,4) NOT NULL,
  MODIFY price         DECIMAL(15,4) NOT NULL;

ALTER TABLE transactions
  MODIFY quantity DECIMAL(15,4) NOT NULL,
  MODIFY price    DECIMAL(15,4) NOT NULL;
