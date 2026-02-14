package sync

import "time"

// Config holds configuration for the sync service
type Config struct {
	// PollInterval is how often to check for wallets needing sync
	PollInterval time.Duration

	// ConcurrentWallets is the max number of wallets to sync concurrently
	ConcurrentWallets int

	// InitialSyncLookback is how far back to look for the first sync (time-based)
	InitialSyncLookback time.Duration

	// Enabled determines if background sync is enabled
	Enabled bool
}

// DefaultConfig returns the default sync configuration
func DefaultConfig() *Config {
	return &Config{
		PollInterval:        5 * time.Minute,
		ConcurrentWallets:   3,
		InitialSyncLookback: 2160 * time.Hour, // 90 days
		Enabled:             true,
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
	if c.InitialSyncLookback <= 0 {
		c.InitialSyncLookback = 2160 * time.Hour
	}
	return nil
}
