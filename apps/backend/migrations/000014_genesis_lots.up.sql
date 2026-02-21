INSERT INTO tax_lots (transaction_id, account_id, asset, quantity_acquired,
                      quantity_remaining, acquired_at, auto_cost_basis_per_unit,
                      auto_cost_basis_source)
SELECT
    sub.first_tx_id,
    ab.account_id,
    ab.asset_id,
    ab.balance,
    ab.balance,
    sub.first_occurred_at,
    COALESCE(rate.latest_usd_rate, 0),
    'genesis_approximation'
FROM account_balances ab
JOIN accounts a ON ab.account_id = a.id
CROSS JOIN LATERAL (
    SELECT t.id as first_tx_id, t.occurred_at as first_occurred_at
    FROM transactions t
    JOIN entries e ON e.transaction_id = t.id
    WHERE e.account_id = ab.account_id AND e.asset_id = ab.asset_id
    ORDER BY t.occurred_at ASC LIMIT 1
) sub
CROSS JOIN LATERAL (
    SELECT e.usd_rate as latest_usd_rate
    FROM entries e
    WHERE e.account_id = ab.account_id AND e.asset_id = ab.asset_id
    ORDER BY e.occurred_at DESC LIMIT 1
) rate
WHERE a.type = 'CRYPTO_WALLET'
  AND ab.balance > 0
  AND NOT EXISTS (
      SELECT 1 FROM tax_lots tl
      WHERE tl.account_id = ab.account_id AND tl.asset = ab.asset_id
  );
