-- The order header.
-- mml:layer oltp
CREATE TABLE orders (
    order_id    BIGINT PRIMARY KEY,
    customer_id BIGINT NOT NULL,
    placed_at   TIMESTAMP,
    status      TEXT NOT NULL CHECK (status IN ('open', 'shipped'))
);

COMMENT ON TABLE public.orders IS 'One order.
@grain: one row per order';

CREATE TABLE order_lines (
    order_id BIGINT NOT NULL REFERENCES orders (order_id),
    line_no  INTEGER NOT NULL,
    amount   NUMERIC(12, 2),
    PRIMARY KEY (order_id, line_no)
);

COMMENT ON COLUMN order_lines.amount IS '@unit: USD';
