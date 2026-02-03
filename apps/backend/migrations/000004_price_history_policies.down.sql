-- Remove policies
SELECT remove_retention_policy('price_history', if_exists => true);
SELECT remove_compression_policy('price_history', if_exists => true);

-- Disable compression
ALTER TABLE price_history SET (timescaledb.compress = false);
