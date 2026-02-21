DELETE FROM tax_lots
WHERE auto_cost_basis_source = 'genesis_approximation'
  AND quantity_remaining = quantity_acquired
  AND NOT EXISTS (SELECT 1 FROM lot_disposals ld WHERE ld.lot_id = tax_lots.id);
