-- Continuous aggregate for daily prices (OHLCV)
CREATE MATERIALIZED VIEW price_history_daily
WITH (timescaledb.continuous) AS
SELECT
    asset_id,
    time_bucket('1 day', time) AS day,
    first(price_usd, time) AS open,
    max(price_usd) AS high,
    min(price_usd) AS low,
    last(price_usd, time) AS close,
    avg(volume_24h) AS avg_volume
FROM price_history
GROUP BY asset_id, time_bucket('1 day', time)
WITH NO DATA;

-- Refresh policy: update every 1 hour, look back 3 days, end 1 hour ago
SELECT add_continuous_aggregate_policy('price_history_daily',
    start_offset => INTERVAL '3 days',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour');
