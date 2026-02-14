-- Drop the last_sync_block column (was used by Alchemy block-based sync, now replaced by Zerion time-based cursor)
ALTER TABLE wallets DROP COLUMN IF EXISTS last_sync_block;
