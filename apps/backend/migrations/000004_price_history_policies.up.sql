-- Compression policy (compress chunks older than 30 days)
ALTER TABLE price_history SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'asset_id'
);

SELECT add_compression_policy('price_history', INTERVAL '30 days');

-- Retention policy (drop data older than 2 years)
SELECT add_retention_policy('price_history', INTERVAL '2 years');
