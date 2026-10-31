-- 7.5-D: seconds from a column every write truncates to the minute.
SELECT date_trunc('second', viewed_at), COUNT(*)
FROM page_views
GROUP BY 1;
