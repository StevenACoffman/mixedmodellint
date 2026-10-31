-- 7.3-A: a plain append to an incrementally loaded table.
INSERT INTO product_sales (product_id, sale_date, units_sold)
SELECT 1, CURRENT_DATE, 5;

-- 7.1-C: upserting on a key that is not the grain.
INSERT INTO product_sales (product_id, sale_date, units_sold)
VALUES (1, CURRENT_DATE, 5)
ON CONFLICT (product_id) DO UPDATE SET units_sold = excluded.units_sold;

-- Passes: idempotent on the grain key.
INSERT INTO product_sales (product_id, sale_date, units_sold)
VALUES (1, CURRENT_DATE, 5)
ON CONFLICT (product_id, sale_date) DO UPDATE SET units_sold = excluded.units_sold;
