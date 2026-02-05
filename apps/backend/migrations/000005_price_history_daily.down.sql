SELECT remove_continuous_aggregate_policy('price_history_daily', if_exists => true);
DROP MATERIALIZED VIEW IF EXISTS price_history_daily;
