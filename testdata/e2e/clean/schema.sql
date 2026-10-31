-- mml:grain one row per order
CREATE TABLE orders (
    order_id  BIGINT PRIMARY KEY,
    placed_at TIMESTAMPTZ NOT NULL
);
