-- 7.2-A: the bonus is summed once per order.
SELECT c.customer_id, SUM(c.signup_bonus)
FROM customers c
JOIN orders o ON o.customer_id = c.customer_id
GROUP BY c.customer_id;

-- Passes: joining the parent preserves the order grain.
SELECT SUM(o.total_usd)
FROM orders o
JOIN customers c ON c.customer_id = o.customer_id;

-- Passes: COUNT(DISTINCT ...) is immune to fan-out.
SELECT COUNT(DISTINCT c.customer_id)
FROM customers c
JOIN orders o ON o.customer_id = c.customer_id;
