-- mml:grain one row per customer
CREATE TABLE customers (
    customer_id   BIGINT PRIMARY KEY,
    signup_bonus  NUMERIC(10, 2) NOT NULL
);

-- mml:grain one row per order
CREATE TABLE orders (
    order_id    BIGINT PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers (customer_id),
    placed_at   TIMESTAMPTZ NOT NULL,
    total_usd   NUMERIC(12, 2) NOT NULL
);

-- mml:grain one row per page view
-- mml:layer event
CREATE TABLE page_views (
    page_view_id BIGINT PRIMARY KEY,
    viewed_at    TIMESTAMPTZ NOT NULL
);

-- mml:grain one row per sale date
CREATE TABLE daily_revenue (
    sale_date   DATE PRIMARY KEY,
    revenue_usd NUMERIC(12, 2) NOT NULL
);

-- mml:grain one row per product per sale date
-- mml:load incremental
CREATE TABLE product_sales (
    product_id BIGINT NOT NULL,
    sale_date  DATE NOT NULL,
    units_sold INTEGER NOT NULL,
    PRIMARY KEY (product_id, sale_date)
);
