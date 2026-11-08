-- mml:grain one row per product per sale date
-- mml:layer analytical
-- mml:load incremental
-- mml:dimensions product_id
CREATE TABLE daily_product_sales (
    product_id       BIGINT NOT NULL,
    sale_date        DATE NOT NULL,
    units_sold       INTEGER NOT NULL,
    revenue_usd_sum  NUMERIC(14, 2) NOT NULL,
    order_line_count INTEGER NOT NULL,
    PRIMARY KEY (product_id, sale_date)
);

COMMENT ON COLUMN daily_product_sales.product_id IS 'The product whose sales the row sums.';
COMMENT ON COLUMN daily_product_sales.sale_date IS 'The calendar day the orders were placed on.';
COMMENT ON COLUMN daily_product_sales.units_sold IS 'Units of the product ordered that day.';
COMMENT ON COLUMN daily_product_sales.revenue_usd_sum IS 'Revenue from those units, in US dollars.';
COMMENT ON COLUMN daily_product_sales.order_line_count IS 'How many order lines contributed to the row.';
