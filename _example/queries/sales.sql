-- Rebuild a day's sales idempotently on the grain key (§7.3).
INSERT INTO daily_product_sales (product_id, sale_date, units_sold, revenue_usd_sum, order_line_count)
SELECT l.product_id, o.placed_at::date, SUM(l.quantity), SUM(l.unit_price_usd * l.quantity), COUNT(*)
FROM order_lines l
JOIN orders o ON o.order_id = l.order_id
WHERE o.placed_at >= $1 AND o.placed_at < $2
GROUP BY l.product_id, o.placed_at::date
ON CONFLICT (product_id, sale_date) DO UPDATE
SET units_sold = excluded.units_sold,
    revenue_usd_sum = excluded.revenue_usd_sum,
    order_line_count = excluded.order_line_count;

-- Average order-line value, from additive components (§8.4).
SELECT sale_date, SUM(revenue_usd_sum)::numeric / NULLIF(SUM(order_line_count), 0)
FROM daily_product_sales
GROUP BY sale_date;
