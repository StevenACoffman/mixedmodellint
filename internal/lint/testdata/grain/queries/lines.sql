-- name: CreateLine :exec
INSERT INTO order_lines (order_id, line_no, order_fee)
VALUES (@order_id, @line_no, @order_fee);
