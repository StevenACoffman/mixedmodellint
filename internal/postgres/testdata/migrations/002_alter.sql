ALTER TABLE orders ADD COLUMN coupon_code VARCHAR(20);
ALTER TABLE orders DROP COLUMN status;
ALTER TABLE orders ALTER COLUMN placed_at TYPE timestamptz;
