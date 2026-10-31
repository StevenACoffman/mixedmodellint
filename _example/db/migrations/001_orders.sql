-- mml:grain one row per customer
CREATE TABLE customers (
    customer_id  BIGINT PRIMARY KEY,
    email        TEXT NOT NULL,
    signed_up_at TIMESTAMPTZ NOT NULL
);

-- mml:grain one row per order
CREATE TABLE orders (
    order_id    BIGINT PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers (customer_id),
    placed_at   TIMESTAMPTZ NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('open', 'shipped', 'cancelled'))
);

-- mml:grain one row per order line
CREATE TABLE order_lines (
    order_id       BIGINT NOT NULL REFERENCES orders (order_id),
    line_no        INTEGER NOT NULL,
    product_id     BIGINT NOT NULL,
    unit_price_usd NUMERIC(12, 2) NOT NULL,
    quantity       INTEGER NOT NULL,
    PRIMARY KEY (order_id, line_no)
);
