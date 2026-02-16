# Real Wallet E2E Sync Test — Base (chain_id 8453)

End-to-end test scenario: register user, add a real Base wallet, perform on-chain operations (MetaMask), verify MoonTrack picks them up via Zerion sync.

> **Base URL**: `http://localhost:8080`
>
> **Prerequisites**:
> - Clean database: `just db-reset && just migrate-up`
> - Services running: `just dev`
> - A MetaMask wallet on Base with some ETH for gas
> - Access to [BaseScan](https://basescan.org) for verification

---

## Variables

| Variable | Source | Description |
|---|---|---|
| `{{TOKEN}}` | Part 1, Step 1 | JWT token |
| `{{WALLET_ID}}` | Part 1, Step 2 | MoonTrack wallet UUID |
| `{{MY_ADDRESS}}` | Your MetaMask | Your Base wallet address (EIP-55 checksum) |
| `{{TX_HASH_1}}` | Part 2, Op 1 | ETH receive tx hash |
| `{{TX_HASH_2}}` | Part 2, Op 2 | Swap tx hash |
| `{{TX_HASH_3}}` | Part 2, Op 3 | USDC transfer out tx hash |

---

## Part 1: Prepare MoonTrack

### Step 1 — Register & Get Token

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email": "e2e-base@moontrack.dev", "password": "TestBase2024!"}' | jq -r '.token')

echo "TOKEN=$TOKEN"
```

**Expected**: `201 Created`, token saved to `$TOKEN`.

### Step 2 — Add Your Base Wallet

Replace `{{MY_ADDRESS}}` with your real Base wallet address (EIP-55 checksum format).

```bash
WALLET_ID=$(curl -s -X POST http://localhost:8080/api/v1/wallets \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Base E2E Wallet",
    "chain_id": 8453,
    "address": "{{MY_ADDRESS}}"
  }' | jq -r '.id')

echo "WALLET_ID=$WALLET_ID"
```

**Expected**: `201 Created` with:
```json
{
  "id": "...",
  "name": "Base E2E Wallet",
  "chain_id": 8453,
  "chain_name": "Base",
  "address": "{{MY_ADDRESS}}",
  "sync_status": "pending"
}
```

### Step 3 — Verify Initial Sync Status

```bash
curl -s http://localhost:8080/api/v1/wallets/$WALLET_ID \
  -H "Authorization: Bearer $TOKEN" | jq '{sync_status, last_sync_at, sync_error}'
```

**Expected**: `sync_status: "pending"`, `last_sync_at: null`.

### Step 4 — Wait for First Sync Cycle

The background sync runs every **5 minutes** with up to 3 concurrent wallets. Wait for the first cycle or restart the backend to trigger immediately:

```bash
# Option A: Wait 5 minutes, then check
sleep 300

# Option B: Restart backend to trigger immediate sync
# (Ctrl+C the backend, then: just backend-run)
```

### Step 5 — Verify Sync Completed

Poll until `sync_status` changes to `"synced"`:

```bash
curl -s http://localhost:8080/api/v1/wallets/$WALLET_ID \
  -H "Authorization: Bearer $TOKEN" | jq '{sync_status, last_sync_at, sync_error}'
```

**Expected**:
```json
{
  "sync_status": "synced",
  "last_sync_at": "2026-02-14T...",
  "sync_error": null
}
```

If `sync_status: "error"`, check `sync_error` field and backend logs.

### Step 6 — Check Existing Transaction History

Zerion initial sync looks back **90 days**. If the wallet has prior activity, those transactions should already appear:

```bash
curl -s "http://localhost:8080/api/v1/transactions?wallet_id=$WALLET_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '{total, transactions: [.transactions[] | {type, status, occurred_at}]}'
```

**Expected**: `200 OK` — list of any historic transactions already on-chain.

### Step 7 — Check Initial Portfolio

```bash
curl -s http://localhost:8080/api/v1/portfolio \
  -H "Authorization: Bearer $TOKEN" | jq .
```

**Expected**: Portfolio reflects current on-chain balances from initial sync.

---

## Part 2: On-Chain Operations (MetaMask)

Perform these operations manually via MetaMask (or another wallet). After each operation, record the **tx_hash**, **amount**, and **timestamp** from BaseScan.

> **Important**: Wait for each transaction to be confirmed on-chain before proceeding.

### Operation 1: Receive ETH on Base

Send ETH to your Base address from another wallet or bridge.

**Steps**:
1. From another wallet (or a bridge like [Base Bridge](https://bridge.base.org)):
   - Send a small amount of ETH (e.g. 0.005 ETH) to `{{MY_ADDRESS}}` on Base
2. Wait for confirmation on [BaseScan](https://basescan.org/address/{{MY_ADDRESS}})
3. Record:
   - `TX_HASH_1`: _______________
   - Amount: _______________
   - Timestamp: _______________

**What MoonTrack should detect**: `transfer_in` transaction

### Operation 2: Swap ETH → USDC via 1inch (Base)

**Steps**:
1. Go to [app.1inch.io](https://app.1inch.io)
2. Connect MetaMask, select **Base** network
3. Swap a portion of ETH → USDC (e.g. 0.002 ETH → ~$5 USDC)
4. Confirm in MetaMask, wait for on-chain confirmation
5. Record:
   - `TX_HASH_2`: _______________
   - ETH spent: _______________
   - USDC received: _______________
   - Gas used (ETH): _______________
   - Timestamp: _______________

**What MoonTrack should detect**: `swap` transaction with:
- `transfers_out`: ETH
- `transfers_in`: USDC
- Protocol name from Zerion (e.g. "1inch")
- Gas fee entries

### Operation 3: Transfer USDC to Another Address

**Steps**:
1. In MetaMask, send some USDC to another address (can be your own second wallet)
   - Amount: e.g. $2 USDC
   - To: any valid Base address
2. Wait for on-chain confirmation
3. Record:
   - `TX_HASH_3`: _______________
   - USDC amount: _______________
   - Recipient: _______________
   - Gas used (ETH): _______________
   - Timestamp: _______________

**What MoonTrack should detect**: `transfer_out` transaction with gas fee entries

---

## Part 3: Verify via MoonTrack API

After each operation, wait for a sync cycle (~5 minutes) or restart the backend.

### 3.1 — Check Sync Status After Operations

```bash
curl -s http://localhost:8080/api/v1/wallets/$WALLET_ID \
  -H "Authorization: Bearer $TOKEN" | jq '{sync_status, last_sync_at}'
```

**Expected**: `sync_status: "synced"`, `last_sync_at` updated to recent time.

### 3.2 — List All Transactions

```bash
curl -s "http://localhost:8080/api/v1/transactions?wallet_id=$WALLET_ID" \
  -H "Authorization: Bearer $TOKEN" | jq .
```

**Expected**: New transactions from Part 2 should appear in the list. Check `total` increased.

### 3.3 — Verify Transfer In (Operation 1)

Find the ETH receive transaction:

```bash
curl -s "http://localhost:8080/api/v1/transactions?wallet_id=$WALLET_ID&type=transfer_in" \
  -H "Authorization: Bearer $TOKEN" | jq '.transactions[] | select(.occurred_at > "2026-02-14")'
```

**Verify**:
- `type`: `"transfer_in"`
- `status`: `"completed"`
- Amount matches what you sent

**Check ledger entries** (get the transaction ID from above, then):

```bash
TX_IN_ID="<paste transaction id>"
curl -s "http://localhost:8080/api/v1/transactions/$TX_IN_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.entries'
```

**Expected entries** (2):
| # | Side | Type |
|---|------|------|
| 1 | DEBIT | asset_increase (wallet) |
| 2 | CREDIT | income |

**Verify**: `SUM(debit) = SUM(credit)`

### 3.4 — Verify Swap (Operation 2)

Find the swap transaction:

```bash
curl -s "http://localhost:8080/api/v1/transactions?wallet_id=$WALLET_ID&type=swap" \
  -H "Authorization: Bearer $TOKEN" | jq '.transactions[] | select(.occurred_at > "2026-02-14")'
```

**Verify**:
- `type`: `"swap"`
- `status`: `"completed"`

**Check ledger entries**:

```bash
TX_SWAP_ID="<paste transaction id>"
curl -s "http://localhost:8080/api/v1/transactions/$TX_SWAP_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.entries'
```

**Expected entries** (4-6):
| # | Side | Type | Description |
|---|------|------|-------------|
| 1 | DEBIT | clearing | ETH sent to swap |
| 2 | CREDIT | asset_decrease | ETH leaves wallet |
| 3 | DEBIT | asset_increase | USDC enters wallet |
| 4 | CREDIT | clearing | USDC received from swap |
| 5 | DEBIT | gas_fee | Gas fee (if present) |
| 6 | CREDIT | asset_decrease | Gas paid from wallet (if present) |

**Verify**:
- `SUM(debit) = SUM(credit)`
- Protocol field is populated (e.g. "1inch")
- Both ETH out and USDC in are recorded

### 3.5 — Verify Transfer Out (Operation 3)

Find the USDC transfer:

```bash
curl -s "http://localhost:8080/api/v1/transactions?wallet_id=$WALLET_ID&type=transfer_out" \
  -H "Authorization: Bearer $TOKEN" | jq '.transactions[] | select(.occurred_at > "2026-02-14")'
```

**Verify**:
- `type`: `"transfer_out"`
- `status`: `"completed"`
- Amount matches USDC sent

**Check ledger entries**:

```bash
TX_OUT_ID="<paste transaction id>"
curl -s "http://localhost:8080/api/v1/transactions/$TX_OUT_ID" \
  -H "Authorization: Bearer $TOKEN" | jq '.entries'
```

**Expected entries** (2-4):
| # | Side | Type | Description |
|---|------|------|-------------|
| 1 | DEBIT | expense | USDC spent |
| 2 | CREDIT | asset_decrease | USDC leaves wallet |
| 3 | DEBIT | gas_fee | Gas fee (if present) |
| 4 | CREDIT | asset_decrease | ETH gas paid (if present) |

**Verify**: `SUM(debit) = SUM(credit)`

### 3.6 — Verify All Transactions Have Status COMPLETED

```bash
curl -s "http://localhost:8080/api/v1/transactions?wallet_id=$WALLET_ID" \
  -H "Authorization: Bearer $TOKEN" \
  | jq '[.transactions[] | .status] | unique'
```

**Expected**: `["completed"]` — no failed or pending transactions.

---

## Part 4: Final Verification

### 4.1 — Portfolio Snapshot

```bash
curl -s http://localhost:8080/api/v1/portfolio \
  -H "Authorization: Bearer $TOKEN" | jq .
```

**Check**:
- `total_usd_value` is non-zero
- `asset_holdings` includes ETH and USDC (if USDC remains)
- `wallet_balances` shows "Base E2E Wallet" with correct assets

### 4.2 — Compare with BaseScan

Open your wallet on BaseScan and compare:

```
https://basescan.org/address/{{MY_ADDRESS}}
```

| Check | MoonTrack (API) | BaseScan | Match? |
|-------|----------------|----------|--------|
| ETH balance | `portfolio → wallet_balances → ETH amount` | Token balance page | |
| USDC balance | `portfolio → wallet_balances → USDC amount` | Token balance page | |
| Transaction count | `transactions → total` | Txn tab count | |

> **Note**: MoonTrack stores amounts in base units. To compare:
> - ETH: divide by 10^18
> - USDC: divide by 10^6

### 4.3 — Verify Swap Protocol Decoding

Check that Zerion decoded the 1inch swap with protocol information:

```bash
curl -s "http://localhost:8080/api/v1/transactions?wallet_id=$WALLET_ID&type=swap" \
  -H "Authorization: Bearer $TOKEN" | jq '.transactions[] | {type, occurred_at, raw_data}'
```

**Expected**: `raw_data` should contain `protocol` field (e.g. `"1inch"`) from Zerion's decoded transaction.

### 4.4 — Ledger Integrity Check

For every transaction, verify double-entry accounting holds:

```bash
# Get all transaction IDs
TX_IDS=$(curl -s "http://localhost:8080/api/v1/transactions?wallet_id=$WALLET_ID&page_size=100" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.transactions[].id')

# Check each transaction's entries are balanced
for TX_ID in $TX_IDS; do
  RESULT=$(curl -s "http://localhost:8080/api/v1/transactions/$TX_ID" \
    -H "Authorization: Bearer $TOKEN")
  TYPE=$(echo "$RESULT" | jq -r '.type')
  DEBITS=$(echo "$RESULT" | jq '[.entries[] | select(.side == "DEBIT") | .amount | tonumber] | add')
  CREDITS=$(echo "$RESULT" | jq '[.entries[] | select(.side == "CREDIT") | .amount | tonumber] | add')
  BALANCED=$([ "$DEBITS" = "$CREDITS" ] && echo "OK" || echo "MISMATCH")
  echo "$TX_ID ($TYPE): debit=$DEBITS credit=$CREDITS [$BALANCED]"
done
```

**Expected**: Every transaction shows `[OK]`.

---

## E2E Coverage Checklist

### Sync Lifecycle
- [ ] Wallet created with `sync_status: "pending"`
- [ ] After first cycle: `sync_status: "syncing"` → `"synced"`
- [ ] `last_sync_at` timestamp updates after each cycle
- [ ] No `sync_error` present
- [ ] Historical transactions (90-day lookback) imported on first sync

### Transaction Handlers
| Handler | Triggered By | Verified |
|---------|-------------|----------|
| `transfer_in` | ETH deposit (Op 1) | [ ] |
| `swap` | 1inch ETH→USDC (Op 2) | [ ] |
| `transfer_out` | USDC send (Op 3) | [ ] |

### Ledger Integrity
- [ ] Every transaction has `status: "completed"`
- [ ] `SUM(debit) = SUM(credit)` for all transactions
- [ ] `transfer_in`: 2 entries (asset_increase + income)
- [ ] `swap`: 4-6 entries (clearing accounts + optional gas)
- [ ] `transfer_out`: 2-4 entries (expense + asset_decrease + optional gas)

### Portfolio Accuracy
- [ ] ETH balance matches BaseScan
- [ ] USDC balance matches BaseScan
- [ ] USD values are calculated (non-zero `total_usd_value`)
- [ ] Wallet breakdown shows correct chain (Base / 8453)

### Zerion Integration
- [ ] Swap protocol name decoded (e.g. "1inch")
- [ ] Token contract addresses populated for ERC-20 transfers
- [ ] Gas fees tracked as separate ledger entries
- [ ] Duplicate transactions not created on re-sync

---

## Troubleshooting

### Sync stuck on "pending"
- Check backend logs for Zerion API errors
- Verify `ZERION_API_KEY` is set in `.env`
- Ensure Base (8453) is in `config/chains.yaml`

### Transactions missing after sync
- Wait for the full 5-minute sync cycle to complete
- Check `last_sync_at` — if it hasn't updated, sync may not have run
- Look at backend logs for classification errors
- Verify the transaction is confirmed on BaseScan (not pending)

### Balance mismatch with BaseScan
- MoonTrack tracks historical transactions, not live balance snapshots
- If wallet had activity before the 90-day lookback window, older transactions won't be imported
- Gas fees may account for small differences — check gas entries in ledger

### Sync status "error"
```bash
curl -s http://localhost:8080/api/v1/wallets/$WALLET_ID \
  -H "Authorization: Bearer $TOKEN" | jq '{sync_status, sync_error}'
```
- Common causes: Zerion API rate limit, invalid API key, network timeout
- Fix the issue and restart backend — next cycle will retry
