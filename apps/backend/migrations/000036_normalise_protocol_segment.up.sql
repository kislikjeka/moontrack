-- Rewrite existing protocol-scoped account codes into the normalised form the
-- constructor now emits (#73).
--
-- Without this the fix would only apply to accounts created from here on: the
-- rows already in the table keep their old spelling, and the next supply or
-- withdraw builds the normalised code, fails to match, and creates a THIRD
-- account for the same position — turning a two-way split into a three-way one.
--
-- The transformation mirrors accountcode.protocolSlug: lowercase, every run of
-- non-alphanumeric runes becomes a single dash, leading/trailing dashes are
-- trimmed, and a segment left empty becomes the explicit 'unknown' sentinel.
-- Chain and asset segments are untouched — they are internal identifiers, not
-- provider display names.

-- A merge is needed when two rows collapse onto the same normalised code. On
-- the observed data the empty-protocol USDC account carries no entries at all
-- (the withdraw that would have used it failed on the zero balance, which is
-- the defect), so it is merged into its named twin rather than kept.
DO $$
DECLARE
    r RECORD;
    normalised TEXT;
    survivor UUID;
BEGIN
    FOR r IN
        SELECT id, code FROM accounts
        WHERE code LIKE 'collateral.%' OR code LIKE 'liability.%'
        ORDER BY code
    LOOP
        -- split_part on '.' is safe here only because the namespace is the
        -- first segment and the protocol the second; the remainder is
        -- reassembled from the original string rather than re-split, so a code
        -- whose old protocol segment contained dots keeps its tail intact.
        normalised :=
            split_part(r.code, '.', 1) || '.' ||
            COALESCE(
                NULLIF(
                    trim(BOTH '-' FROM regexp_replace(lower(split_part(r.code, '.', 2)), '[^a-z0-9]+', '-', 'g')),
                    ''
                ),
                'unknown'
            ) || '.' ||
            substring(r.code FROM length(split_part(r.code, '.', 1)) + length(split_part(r.code, '.', 2)) + 3);

        CONTINUE WHEN normalised = r.code;

        SELECT id INTO survivor FROM accounts WHERE code = normalised AND id <> r.id;

        IF survivor IS NULL THEN
            -- No collision: the account simply takes its normalised name.
            UPDATE accounts SET code = normalised WHERE id = r.id;
        ELSE
            -- Collision: move everything this account carries onto the survivor
            -- and drop it, so the position ends up on exactly one account.
            UPDATE entries SET account_id = survivor WHERE account_id = r.id;
            UPDATE tax_lots SET account_id = survivor WHERE account_id = r.id;

            -- Balances are per (account, asset); fold them together.
            INSERT INTO account_balances (account_id, asset_id, balance, usd_value, last_updated)
            SELECT survivor, ab.asset_id, ab.balance, ab.usd_value, now()
            FROM account_balances ab WHERE ab.account_id = r.id
            ON CONFLICT (account_id, asset_id) DO UPDATE
                SET balance = account_balances.balance + EXCLUDED.balance,
                    usd_value = account_balances.usd_value + EXCLUDED.usd_value,
                    last_updated = now();

            DELETE FROM account_balances WHERE account_id = r.id;
            DELETE FROM accounts WHERE id = r.id;
        END IF;
    END LOOP;
END $$;
