-- mml:grain one row per order
-- mml:definition A request from a customer to buy goods, placed at checkout and fulfilled as one shipment.
-- mml:owner team:orders
-- mml:escalation role:head-of-commerce
CREATE TABLE orders (
    order_id  BIGINT PRIMARY KEY,
    placed_at TIMESTAMPTZ NOT NULL
);
