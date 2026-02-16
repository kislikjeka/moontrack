package zerion_test

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kislikjeka/moontrack/internal/infra/gateway/zerion"
	"github.com/kislikjeka/moontrack/internal/platform/sync"
)

// =============================================================================
// Interface Compliance
// =============================================================================

func TestSyncAdapter_ImplementsTransactionDataProvider(t *testing.T) {
	var _ sync.TransactionDataProvider = (*zerion.SyncAdapter)(nil)
}

// =============================================================================
// Unsupported Chain
// =============================================================================

func TestSyncAdapter_UnsupportedChain(t *testing.T) {
	client := zerion.NewClient("key", testLogger())
	adapter := zerion.NewSyncAdapter(client)

	_, err := adapter.GetTransactions(context.Background(), "0xtest", 999999, time.Now())
	assert.ErrorIs(t, err, zerion.ErrUnsupportedChain)
}

// =============================================================================
// Full Conversion Test
// =============================================================================

func TestSyncAdapter_FullConversion(t *testing.T) {
	ethPrice := 3500.12
	feePrice := 3500.12

	txData := zerion.TransactionResponse{
		Data: []zerion.TransactionData{
			{
				Type: "transactions",
				ID:   "zerion-tx-1",
				Attributes: zerion.TransactionAttributes{
					OperationType: "trade",
					Hash:          "0xabc123",
					MinedAt:       "2024-06-15T12:00:00Z",
					SentFrom:      "0xSender",
					SentTo:        "0xRecipient",
					Status:        "confirmed",
					Fee: &zerion.Fee{
						FungibleInfo: &zerion.FungibleInfo{
							Symbol: "ETH",
							Implementations: []zerion.Implementation{
								{ChainID: "ethereum", Address: "0x0000000000000000000000000000000000000000", Decimals: 18},
							},
						},
						Quantity: zerion.Quantity{Int: "2100000000000000", Decimals: 18},
						Price:    &feePrice,
					},
					Transfers: []zerion.ZTransfer{
						{
							FungibleInfo: &zerion.FungibleInfo{
								Symbol: "ETH",
								Implementations: []zerion.Implementation{
									{ChainID: "ethereum", Address: "0x0000000000000000000000000000000000000000", Decimals: 18},
								},
							},
							Direction: "out",
							Quantity:  zerion.Quantity{Int: "1000000000000000000", Decimals: 18},
							Sender:    "0xSender",
							Recipient: "0xRecipient",
							Price:     &ethPrice,
						},
						{
							FungibleInfo: &zerion.FungibleInfo{
								Symbol: "USDC",
								Implementations: []zerion.Implementation{
									{ChainID: "ethereum", Address: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", Decimals: 6},
								},
							},
							Direction: "in",
							Quantity:  zerion.Quantity{Int: "3500120000", Decimals: 6},
							Sender:    "0xPool",
							Recipient: "0xSender",
							Price:     nil,
						},
					},
					ApplicationMD: &zerion.ApplicationMeta{Name: "Uniswap V3"},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(txData)
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)
	adapter := zerion.NewSyncAdapter(client)

	txs, err := adapter.GetTransactions(context.Background(), "0xSender", 1, time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	require.Len(t, txs, 1)

	tx := txs[0]
	assert.Equal(t, "zerion-tx-1", tx.ID)
	assert.Equal(t, "0xabc123", tx.TxHash)
	assert.Equal(t, int64(1), tx.ChainID)
	assert.Equal(t, sync.OperationType("trade"), tx.OperationType)
	assert.Equal(t, "Uniswap V3", tx.Protocol)
	assert.Equal(t, "confirmed", tx.Status)
	assert.Equal(t, time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC), tx.MinedAt)

	// Transfers
	require.Len(t, tx.Transfers, 2)

	// First transfer: ETH out
	ethTransfer := tx.Transfers[0]
	assert.Equal(t, "ETH", ethTransfer.AssetSymbol)
	assert.Equal(t, "0x0000000000000000000000000000000000000000", ethTransfer.ContractAddress)
	assert.Equal(t, 18, ethTransfer.Decimals)
	assert.Equal(t, 0, ethTransfer.Amount.Cmp(big.NewInt(1000000000000000000)))
	assert.Equal(t, sync.DirectionOut, ethTransfer.Direction)
	assert.Equal(t, "0xsender", ethTransfer.Sender)
	assert.Equal(t, "0xrecipient", ethTransfer.Recipient)
	// 3500.12 * 1e8 = 350012000000
	assert.Equal(t, 0, ethTransfer.USDPrice.Cmp(big.NewInt(350012000000)))

	// Second transfer: USDC in
	usdcTransfer := tx.Transfers[1]
	assert.Equal(t, "USDC", usdcTransfer.AssetSymbol)
	assert.Equal(t, "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", usdcTransfer.ContractAddress) // lowercased
	assert.Equal(t, 6, usdcTransfer.Decimals)
	assert.Equal(t, 0, usdcTransfer.Amount.Cmp(big.NewInt(3500120000)))
	assert.Equal(t, sync.DirectionIn, usdcTransfer.Direction)
	assert.Nil(t, usdcTransfer.USDPrice) // No price available

	// Fee
	require.NotNil(t, tx.Fee)
	assert.Equal(t, "ETH", tx.Fee.AssetSymbol)
	assert.Equal(t, 0, tx.Fee.Amount.Cmp(big.NewInt(2100000000000000)))
	assert.Equal(t, 18, tx.Fee.Decimals)
	assert.Equal(t, 0, tx.Fee.USDPrice.Cmp(big.NewInt(350012000000)))
}

// =============================================================================
// Nil Safety Tests
// =============================================================================

func TestSyncAdapter_NilFungibleInfo(t *testing.T) {
	txData := zerion.TransactionResponse{
		Data: []zerion.TransactionData{
			{
				ID: "tx-nil-fungible",
				Attributes: zerion.TransactionAttributes{
					OperationType: "receive",
					Hash:          "0xdef",
					MinedAt:       "2024-01-01T00:00:00Z",
					Status:        "confirmed",
					Transfers: []zerion.ZTransfer{
						{
							FungibleInfo: nil, // No fungible info
							Direction:    "in",
							Quantity:     zerion.Quantity{Int: "1000"},
							Sender:       "0xA",
							Recipient:    "0xB",
						},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(txData)
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)
	adapter := zerion.NewSyncAdapter(client)

	txs, err := adapter.GetTransactions(context.Background(), "0xB", 1, time.Time{})
	require.NoError(t, err)
	require.Len(t, txs, 1)

	transfer := txs[0].Transfers[0]
	assert.Equal(t, "", transfer.AssetSymbol)
	assert.Equal(t, "", transfer.ContractAddress)
	assert.Equal(t, 0, transfer.Decimals)
	assert.Equal(t, 0, transfer.Amount.Cmp(big.NewInt(1000)))
}

func TestSyncAdapter_NilFee(t *testing.T) {
	txData := zerion.TransactionResponse{
		Data: []zerion.TransactionData{
			{
				ID: "tx-nil-fee",
				Attributes: zerion.TransactionAttributes{
					OperationType: "receive",
					Hash:          "0xfee",
					MinedAt:       "2024-01-01T00:00:00Z",
					Status:        "confirmed",
					Fee:           nil,
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(txData)
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)
	adapter := zerion.NewSyncAdapter(client)

	txs, err := adapter.GetTransactions(context.Background(), "0xtest", 1, time.Time{})
	require.NoError(t, err)
	require.Len(t, txs, 1)
	assert.Nil(t, txs[0].Fee)
}

func TestSyncAdapter_NilApplicationMetadata(t *testing.T) {
	txData := zerion.TransactionResponse{
		Data: []zerion.TransactionData{
			{
				ID: "tx-nil-app",
				Attributes: zerion.TransactionAttributes{
					OperationType: "send",
					Hash:          "0xapp",
					MinedAt:       "2024-01-01T00:00:00Z",
					Status:        "confirmed",
					ApplicationMD: nil,
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(txData)
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)
	adapter := zerion.NewSyncAdapter(client)

	txs, err := adapter.GetTransactions(context.Background(), "0xtest", 1, time.Time{})
	require.NoError(t, err)
	require.Len(t, txs, 1)
	assert.Equal(t, "", txs[0].Protocol)
}

// =============================================================================
// Empty Quantity.Int Handling
// =============================================================================

func TestSyncAdapter_EmptyQuantityInt(t *testing.T) {
	txData := zerion.TransactionResponse{
		Data: []zerion.TransactionData{
			{
				ID: "tx-empty-qty",
				Attributes: zerion.TransactionAttributes{
					OperationType: "receive",
					Hash:          "0xqty",
					MinedAt:       "2024-01-01T00:00:00Z",
					Status:        "confirmed",
					Transfers: []zerion.ZTransfer{
						{
							FungibleInfo: &zerion.FungibleInfo{Symbol: "ETH"},
							Direction:    "in",
							Quantity:     zerion.Quantity{Int: ""}, // empty
							Sender:       "0xa",
							Recipient:    "0xb",
						},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(txData)
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)
	adapter := zerion.NewSyncAdapter(client)

	txs, err := adapter.GetTransactions(context.Background(), "0xb", 1, time.Time{})
	require.NoError(t, err)
	require.Len(t, txs, 1)
	assert.Equal(t, 0, txs[0].Transfers[0].Amount.Cmp(big.NewInt(0)))
}

// =============================================================================
// Chain Mapping in Adapter
// =============================================================================

func TestSyncAdapter_AllSupportedChains(t *testing.T) {
	for chainID, chainName := range zerion.IDToZerionChain {
		t.Run(chainName, func(t *testing.T) {
			var receivedURL string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedURL = r.URL.String()
				json.NewEncoder(w).Encode(zerion.TransactionResponse{})
			}))
			defer server.Close()

			client := zerion.NewClient("key", testLogger())
			client.SetBaseURL(server.URL)
			adapter := zerion.NewSyncAdapter(client)

			_, err := adapter.GetTransactions(context.Background(), "0xtest", chainID, time.Time{})
			require.NoError(t, err)
			assert.Contains(t, receivedURL, "filter%5Bchain_ids%5D="+chainName)
		})
	}
}

// =============================================================================
// Direction Mapping
// =============================================================================

func TestSyncAdapter_DirectionMapping(t *testing.T) {
	tests := []struct {
		zerionDir string
		expected  sync.TransferDirection
	}{
		{"in", sync.DirectionIn},
		{"out", sync.DirectionOut},
		{"unknown", sync.DirectionOut}, // defaults to out
	}

	for _, tt := range tests {
		t.Run(tt.zerionDir, func(t *testing.T) {
			txData := zerion.TransactionResponse{
				Data: []zerion.TransactionData{
					{
						ID: "tx-dir",
						Attributes: zerion.TransactionAttributes{
							OperationType: "send",
							Hash:          "0xdir",
							MinedAt:       "2024-01-01T00:00:00Z",
							Status:        "confirmed",
							Transfers: []zerion.ZTransfer{
								{
									Direction: tt.zerionDir,
									Quantity:  zerion.Quantity{Int: "100"},
									Sender:    "0xa",
									Recipient: "0xb",
								},
							},
						},
					},
				},
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(txData)
			}))
			defer server.Close()

			client := zerion.NewClient("key", testLogger())
			client.SetBaseURL(server.URL)
			adapter := zerion.NewSyncAdapter(client)

			txs, err := adapter.GetTransactions(context.Background(), "0xtest", 1, time.Time{})
			require.NoError(t, err)
			require.Len(t, txs, 1)
			assert.Equal(t, tt.expected, txs[0].Transfers[0].Direction)
		})
	}
}

// =============================================================================
// Skip Invalid Transactions
// =============================================================================

func TestSyncAdapter_SkipsInvalidMinedAt(t *testing.T) {
	txData := zerion.TransactionResponse{
		Data: []zerion.TransactionData{
			{
				ID: "tx-bad-time",
				Attributes: zerion.TransactionAttributes{
					OperationType: "send",
					Hash:          "0xbad",
					MinedAt:       "not-a-time",
					Status:        "confirmed",
				},
			},
			{
				ID: "tx-good",
				Attributes: zerion.TransactionAttributes{
					OperationType: "receive",
					Hash:          "0xgood",
					MinedAt:       "2024-01-01T00:00:00Z",
					Status:        "confirmed",
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(txData)
	}))
	defer server.Close()

	client := zerion.NewClient("key", testLogger())
	client.SetBaseURL(server.URL)
	adapter := zerion.NewSyncAdapter(client)

	txs, err := adapter.GetTransactions(context.Background(), "0xtest", 1, time.Time{})
	require.NoError(t, err)
	// Bad transaction should be skipped, only good one remains
	require.Len(t, txs, 1)
	assert.Equal(t, "tx-good", txs[0].ID)
}
