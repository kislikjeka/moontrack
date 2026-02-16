-- Re-add the last_sync_block column (Alchemy block-based sync, deprecated)
ALTER TABLE wallets ADD COLUMN last_sync_block BIGINT;
