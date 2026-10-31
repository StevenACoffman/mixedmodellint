-- 7.3-D: readers deduplicate orders on its grain key.
SELECT order_id, total_usd
FROM (
    SELECT o.*, ROW_NUMBER() OVER (PARTITION BY o.order_id ORDER BY o.placed_at) AS rn
    FROM orders o
) ranked
WHERE rn = 1;

-- 7.5-D: hourly buckets from a date column.
SELECT date_trunc('hour', sale_date), SUM(revenue_usd)
FROM daily_revenue
GROUP BY 1;

-- Passes: monthly buckets from a date column are coarser.
SELECT date_trunc('month', sale_date), SUM(revenue_usd)
FROM daily_revenue
GROUP BY 1;
