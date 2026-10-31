-- mml:grain one row per order per line_no
CREATE TABLE order_lines (
    order_id  BIGINT NOT NULL REFERENCES orders (order_id),
    line_no   INTEGER NOT NULL,
    order_fee NUMERIC(10, 2) NOT NULL,
    PRIMARY KEY (order_id, line_no)
);
