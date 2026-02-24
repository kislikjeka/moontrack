package zerion

import "errors"

// ErrUnsupportedChain is returned when a chain ID has no Zerion mapping
var ErrUnsupportedChain = errors.New("unsupported chain for Zerion API")

// TransactionResponse is the top-level Zerion API response for wallet transactions
type TransactionResponse struct {
	Links Links             `json:"links"`
	Data  []TransactionData `json:"data"`
}

// Links contains pagination URLs
type Links struct {
	Self string `json:"self"`
	Next string `json:"next"`
}

// TransactionData wraps a single transaction with its type and ID
type TransactionData struct {
	Type          string                `json:"type"`
	ID            string                `json:"id"`
	Attributes    TransactionAttributes `json:"attributes"`
	Relationships Relationships         `json:"relationships"`
}

// Relationships contains relationship data for a transaction
type Relationships struct {
	Chain ChainRelation `json:"chain"`
}

// ChainRelation wraps chain relationship data
type ChainRelation struct {
	Data ChainData `json:"data"`
}

// ChainData identifies the chain for a transaction
type ChainData struct {
	Type string `json:"type"` // e.g. "chains"
	ID   string `json:"id"`   // e.g. "base", "ethereum"
}

// TransactionAttributes contains the decoded transaction fields
type TransactionAttributes struct {
	OperationType string           `json:"operation_type"`
	Hash          string           `json:"hash"`
	MinedAt       string           `json:"mined_at"` // RFC3339
	SentFrom      string           `json:"sent_from"`
	SentTo        string           `json:"sent_to"`
	Status        string           `json:"status"`
	Nonce         int              `json:"nonce"`
	Fee           *Fee             `json:"fee"`
	Transfers     []ZTransfer      `json:"transfers"`
	Approvals     []Approval       `json:"approvals"`
	ApplicationMD *ApplicationMeta `json:"application_metadata"`
}

// Fee contains the transaction gas fee
type Fee struct {
	FungibleInfo *FungibleInfo `json:"fungible_info"`
	Quantity     Quantity      `json:"quantity"`
	Price        *float64      `json:"price"` // USD price per unit, nil if unavailable
}

// ZTransfer represents a single token movement in a Zerion transaction
type ZTransfer struct {
	FungibleInfo *FungibleInfo `json:"fungible_info"`
	NftInfo      *NftInfo      `json:"nft_info"` // non-nil for NFT transfers
	Direction    string        `json:"direction"` // "in" or "out"
	Quantity     Quantity      `json:"quantity"`
	Sender       string        `json:"sender"`
	Recipient    string        `json:"recipient"`
	Price        *float64      `json:"price"` // USD price per unit, nil if unavailable
}

// NftInfo describes an NFT involved in a transfer
type NftInfo struct {
	ContractAddress string `json:"contract_address"`
	TokenID         string `json:"token_id"`
	Name            string `json:"name"`
	Interface       string `json:"interface"` // "erc721" or "erc1155"
}

// FungibleInfo describes the token involved in a transfer or fee
type FungibleInfo struct {
	Name            string           `json:"name"`
	Symbol          string           `json:"symbol"`
	Icon            *IconInfo        `json:"icon"`
	Implementations []Implementation `json:"implementations"`
}

// ImplementationByChain returns the Implementation for the given chain name, or nil if not found.
func (f *FungibleInfo) ImplementationByChain(chain string) *Implementation {
	for i := range f.Implementations {
		if f.Implementations[i].ChainID == chain {
			return &f.Implementations[i]
		}
	}
	return nil
}

// IconInfo holds token icon URL
type IconInfo struct {
	URL string `json:"url"`
}

// Implementation holds chain-specific contract info
type Implementation struct {
	ChainID  string `json:"chain_id"`
	Address  string `json:"address"`
	Decimals int    `json:"decimals"`
}

// Quantity represents an amount with numeric and decimal string forms
type Quantity struct {
	Int      string  `json:"int"`      // Integer string in base units (e.g. "1000000")
	Decimals int     `json:"decimals"` // Number of decimals
	Float    float64 `json:"float"`    // Human-readable float (for display only, NEVER use for math)
	Numeric  string  `json:"numeric"`  // Decimal string representation
}

// Approval represents a token approval in a transaction
type Approval struct {
	FungibleInfo *FungibleInfo `json:"fungible_info"`
	Quantity     Quantity      `json:"quantity"`
	Sender       string        `json:"sender"`
}

// ApplicationMeta contains protocol/dapp information
type ApplicationMeta struct {
	Name string `json:"name"`
	Icon *IconInfo `json:"icon"`
}

// PositionResponse is the Zerion API response for wallet positions
type PositionResponse struct {
	Links Links          `json:"links"`
	Data  []PositionData `json:"data"`
}

// PositionData wraps a single position with its type and ID
type PositionData struct {
	Type          string             `json:"type"`
	ID            string             `json:"id"`
	Attributes    PositionAttributes `json:"attributes"`
	Relationships Relationships      `json:"relationships"`
}

// PositionAttributes contains the position fields
type PositionAttributes struct {
	PositionType string        `json:"position_type"`
	Quantity     Quantity      `json:"quantity"`
	Value        *float64      `json:"value"`
	Price        float64       `json:"price"`
	FungibleInfo *FungibleInfo `json:"fungible_info"`
}
