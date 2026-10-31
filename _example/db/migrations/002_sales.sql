-- mml:grain one row per product per sale date
-- mml:layer analytical
-- mml:load incremental
CREATE TABLE daily_product_sales (
    product_id       BIGINT NOT NULL,
    sale_date        DATE NOT NULL,
    units_sold       INTEGER NOT NULL,
    revenue_usd_sum  NUMERIC(14, 2) NOT NULL,
    order_line_count INTEGER NOT NULL,
    PRIMARY KEY (product_id, sale_date)
);
