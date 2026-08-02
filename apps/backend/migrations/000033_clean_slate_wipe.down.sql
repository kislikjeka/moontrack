-- The wipe is not reversible: the rows are gone and this file cannot invent
-- them back. Rolling back to 000032 restores the SCHEMA of the expand phase,
-- and the data is restored by re-syncing from the provider, which is where it
-- came from in the first place.
--
-- This is a no-op rather than an error so that `migrate down` through this
-- version works. Making it fail would strand the schema at 000033 with no way
-- back, which is worse than an honest no-op: the thing a rollback needs to undo
-- here is the schema change in 000034, not this deletion.
SELECT 1;
