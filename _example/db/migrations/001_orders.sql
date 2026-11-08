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

COMMENT ON COLUMN customers.customer_id IS 'Identifies the customer in every system that knows them.';
COMMENT ON COLUMN customers.email IS 'Where we send receipts; the customer may change it.';
COMMENT ON COLUMN customers.signed_up_at IS 'When the customer finished creating their account.';
COMMENT ON COLUMN orders.order_id IS 'Identifies the order from checkout through delivery.';
COMMENT ON COLUMN orders.customer_id IS 'The customer who placed and pays for the order.';
COMMENT ON COLUMN orders.placed_at IS 'When the customer confirmed the order at checkout.';
COMMENT ON COLUMN orders.status IS 'Where the order is in fulfilment: open, shipped or cancelled.';
COMMENT ON COLUMN order_lines.order_id IS 'The order this line belongs to.';
COMMENT ON COLUMN order_lines.line_no IS 'Position of the line within its order, from one.';
COMMENT ON COLUMN order_lines.product_id IS 'The product the customer is buying on this line.';
COMMENT ON COLUMN order_lines.unit_price_usd IS 'Price of one unit when the order was placed, in US dollars.';
COMMENT ON COLUMN order_lines.quantity IS 'How many units of the product the customer ordered.';
