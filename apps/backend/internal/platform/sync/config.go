package sync

import "time"

// Config holds configuration for the sync service
type Config struct {
	// PollInterval is how often to check for wallets needing sync
	PollInterval time.Duration

	// ConcurrentWallets is the max number of wallets to sync concurrently
	ConcurrentWallets int

	// InitialSyncBlockLookback is how many blocks to look back for initial sync
	// Set to 0 to sync from genesis (not recommended for mainnet)
	InitialSyncBlockLookback int64

	// MaxBlocksPerSync is the max blocks to sync in one batch
	MaxBlocksPerSync int64

	// Enabled determines if background sync is enabled
	Enabled bool
}

// DefaultConfig returns the default sync configuration
func DefaultConfig() *Config {
	return &Config{
		PollInterval:             5 * time.Minute,
		ConcurrentWallets:        3,
		InitialSyncBlockLookback: 1000000, // ~100 days on Ethereum
		MaxBlocksPerSync:         10000,
		Enabled:                  true,
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.PollInterval <= 0 {
		c.PollInterval = 5 * time.Minute
	}
	if c.ConcurrentWallets <= 0 {
		c.ConcurrentWallets = 3
	}
	if c.MaxBlocksPerSync <= 0 {
		c.MaxBlocksPerSync = 10000
	}
	return nil
}
