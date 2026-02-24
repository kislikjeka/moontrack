-- Fix genesis lot dates: lots created by genesis_balance transactions
-- had acquired_at = 0001-01-01 because the handler didn't set OccurredAt on entries.
UPDATE tax_lots tl
SET acquired_at = t.occurred_at
FROM transactions t
WHERE tl.transaction_id = t.id
  AND t.type = 'genesis_balance'
  AND tl.acquired_at < '0002-01-01';
