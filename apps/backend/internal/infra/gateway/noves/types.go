package noves

import "encoding/json"

// TransactionsResponse is the top-level Noves Translate v2 response for the
// per-chain transactions endpoint (GET /evm/{chain}/txs/{address}).
type TransactionsResponse struct {
	Items       []Transaction `json:"items"`
	PageSize    int           `json:"pageSize"`
	HasNextPage bool          `json:"hasNextPage"`
	NextPageURL string        `json:"nextPageUrl"`
}

// Transaction is a single classified Noves v2 transaction.
type Transaction struct {
	TxTypeVersion      int                `json:"txTypeVersion"`
	Chain              string             `json:"chain"`
	AccountAddress     string             `json:"accountAddress"`
	ClassificationData ClassificationData `json:"classificationData"`
	RawTransactionData RawTransactionData `json:"rawTransactionData"`
}

// ClassificationData holds Noves' rich classification of a transaction: the
// operation type, a human description, the (often null) protocol, and the
// per-direction transfer legs.
type ClassificationData struct {
	Type        string     `json:"type"`
	Source      Source     `json:"source"`
	Description string     `json:"description"`
	Protocol    Protocol   `json:"protocol"`
	Sent        []Transfer `json:"sent"`
	Received    []Transfer `json:"received"`
}

// Source describes how the transaction was classified ("human", "inference").
type Source struct {
	Type string `json:"type"`
}

// Protocol names the dapp/protocol. Name is frequently null on real data, so
// callers derive protocol from party/nft names as a fallback.
type Protocol struct {
	Name *string `json:"name"`
}

// Transfer is a single token (or NFT) movement leg. Fungible legs carry Token;
// NFT legs (e.g. a Uniswap V3 position) carry NFT and no Token.
type Transfer struct {
	Action string `json:"action"`
	From   Party  `json:"from"`
	To     Party  `json:"to"`
	Amount string `json:"amount"`
	Token  *Token `json:"token"`
	NFT    *NFT   `json:"nft"`
}

// Party is a from/to endpoint. Both fields are nullable on real data (e.g. the
// gas sink has a null To address+name).
type Party struct {
	Name    *string `json:"name"`
	Address *string `json:"address"`
}

// Token is a fungible token descriptor. Address is a hex contract address for
// ERC-20s, or a symbol-as-address sentinel for the native coin (e.g. "ETH").
type Token struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
}

// NFT is a non-fungible token descriptor. ID can exceed int64 and appears as
// either a JSON string or number on real data, so it is decoded as json.Number.
type NFT struct {
	Name    string      `json:"name"`
	Symbol  string      `json:"symbol"`
	Address string      `json:"address"`
	ID      json.Number `json:"id"`
}

// RawTransactionData holds the on-chain envelope: hash, addresses, block,
// timestamp and the gas fee. Extra fields (gasUsed, l1Gas, …) are ignored.
type RawTransactionData struct {
	TransactionHash string `json:"transactionHash"`
	FromAddress     string `json:"fromAddress"`
	ToAddress       string `json:"toAddress"`
	BlockNumber     int64  `json:"blockNumber"`
	Timestamp       int64  `json:"timestamp"`
	TransactionFee  Fee    `json:"transactionFee"`
}

// Fee is the gas fee, as a decimal amount in a (usually native) token. Amount is
// json.Number so it decodes whether the upstream emits a JSON string or number.
type Fee struct {
	Amount json.Number `json:"amount"`
	Token  *Token      `json:"token"`
}

// BalanceItem is a single token balance from the per-chain balances endpoint
// (GET /evm/{chain}/tokens/balancesOf/{address}). The endpoint returns a
// top-level JSON array of these. Balance is a decimal string (like transfer
// amounts); the token mirrors the transfer Token shape (native coin uses the
// symbol-as-address sentinel, e.g. address == "ETH").
type BalanceItem struct {
	Balance  string        `json:"balance"`
	USDValue *json.Number  `json:"usdValue"`
	Token    *BalanceToken `json:"token"`
}

// BalanceToken is the token descriptor inside a BalanceItem. It carries an
// optional Price (nullable) in addition to the fungible Token fields; MoonTrack
// keeps its own price pipeline, so Price is only used as a best-effort hint.
type BalanceToken struct {
	Symbol   string       `json:"symbol"`
	Name     string       `json:"name"`
	Decimals int          `json:"decimals"`
	Address  string       `json:"address"`
	Price    *json.Number `json:"price"`
}

// balancesErrorEnvelope is the non-array error body the balances endpoint returns
// for degenerate cases (e.g. a wallet with too many ERC-20 balances). It decodes
// a JSON object with a single "detail" field, which we surface as an error rather
// than crash on the type mismatch against the expected array.
type balancesErrorEnvelope struct {
	Detail string `json:"detail"`
}
