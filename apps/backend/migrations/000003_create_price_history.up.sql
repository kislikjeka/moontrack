-- Price history table (TimescaleDB hypertable)
CREATE TABLE price_history (
    time TIMESTAMPTZ NOT NULL,
    asset_id UUID NOT NULL REFERENCES assets(id),
    price_usd NUMERIC(78,0) NOT NULL,
    volume_24h NUMERIC(78,0),
    market_cap NUMERIC(78,0),
    source VARCHAR(20) NOT NULL DEFAULT 'coingecko',

    PRIMARY KEY (asset_id, time)
);

-- Convert to hypertable (7-day chunks)
SELECT create_hypertable('price_history', 'time', chunk_time_interval => INTERVAL '7 days');

-- Create index for time-based queries
CREATE INDEX idx_price_history_time ON price_history(time DESC);
CREATE INDEX idx_price_history_asset_time ON price_history(asset_id, time DESC);
