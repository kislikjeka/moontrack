package transactions

// TransactionListItem represents a transaction in list view
type TransactionListItem struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	TypeLabel     string `json:"type_label"` // "Income", "Outcome", "Adjustment"
	AssetID       string `json:"asset_id"`
	AssetSymbol   string `json:"asset_symbol"`   // "BTC", "ETH"
	Amount        string `json:"amount"`         // Base units as string
	DisplayAmount string `json:"display_amount"` // "0.5 BTC"
	Direction     string `json:"direction"`      // "in", "out", "adjustment"
	WalletID      string `json:"wallet_id"`
	WalletName    string `json:"wallet_name"` // "My Hardware Wallet"
	Status        string `json:"status"`
	OccurredAt    string `json:"occurred_at"`
	USDValue      string `json:"usd_value,omitempty"`
}

// TransactionDetail represents a transaction in detail view
type TransactionDetail struct {
	TransactionListItem
	Source     string                 `json:"source"`
	ExternalID *string                `json:"external_id,omitempty"`
	RecordedAt string                 `json:"recorded_at"`
	Notes      string                 `json:"notes,omitempty"`
	RawData    map[string]interface{} `json:"raw_data,omitempty"`
	Entries    []EntryResponse        `json:"entries"`
}

// EntryResponse represents a ledger entry in detail view
type EntryResponse struct {
	ID            string `json:"id"`
	AccountCode   string `json:"account_code"`  // "wallet:btc:abc123"
	AccountLabel  string `json:"account_label"` // "My Wallet - BTC"
	DebitCredit   string `json:"debit_credit"`  // "DEBIT" or "CREDIT"
	EntryType     string `json:"entry_type"`    // "asset_increase", "income"
	Amount        string `json:"amount"`
	DisplayAmount string `json:"display_amount"`
	AssetID       string `json:"asset_id"`
	AssetSymbol   string `json:"asset_symbol"`
	USDValue      string `json:"usd_value,omitempty"`
}
