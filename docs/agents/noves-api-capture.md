# Noves API capture rules

Rules for working with the **Noves Translate API** key when you need to capture
real responses (e.g. to build or refresh adapter test fixtures under
`apps/backend/internal/infra/gateway/noves/testdata/`).

## Hard rules

1. **Key handling.** Read the key from the root `.env` (`NOVES_API_KEY`). Never
   print, log, echo, or commit it. Never paste it into a message, a test, or a
   fixture. Auth is the header `apiKey: <key>` (not Basic auth).
2. **Request budget.** Treat the key as rate-limited and metered. Cap any
   capture session at **≤ 12 requests total**. Enforce the cap with a counter
   that refuses past the limit; add a ≥ 500 ms delay between requests.
3. **Endpoints only.** Restrict captures to:
   - `GET /evm/chains` (health / chain list)
   - `GET /evm/{chain}/txs/{address}` (paginated tx list)
   - `GET /evm/{chain}/tx/{hash}` (single tx)
4. **Do not chase pagination to genesis.** Keep `pageSize` small and fetch at
   most 1–2 pages. The collector owns the real cursor; captures only need
   representative pages.
5. **Reuse before re-querying.** Existing fixtures live in the `noves/testdata/`
   directory. Slice and inspect them with `jq` (or read them) rather than
   issuing new API calls. Only capture a genuinely new classification shape you
   cannot derive from what you already have.

## Fixtures

Fixtures are real, unmodified single-transaction JSON objects (top-level
`{txTypeVersion, chain, accountAddress, classificationData, rawTransactionData}`)
unless a filename/comment marks them synthetic. Synthetic fixtures are derived
from real ones (e.g. a pure-send bridge leg = a real round-trip minus its
`received[]`) and are used only for cases real capture did not surface
(both-direction `unclassified`, an amount with excess precision).

The adapter that consumes these lives in `apps/backend/internal/infra/gateway/noves/`.
