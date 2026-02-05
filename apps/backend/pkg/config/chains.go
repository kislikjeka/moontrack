package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Chain represents a supported EVM chain configuration
type Chain struct {
	ChainID           int64  `yaml:"chain_id"`
	Name              string `yaml:"name"`
	AlchemyNetwork    string `yaml:"alchemy_network"`
	NativeAsset       string `yaml:"native_asset"`
	NativeDecimals    int    `yaml:"native_decimals"`
	NativeCoinGeckoID string `yaml:"native_coingecko_id"`
}

// ChainsConfig holds all supported chains
type ChainsConfig struct {
	Chains []Chain `yaml:"chains"`

	// Lookup maps for fast access
	byChainID map[int64]*Chain
}

// LoadChainsConfig loads chain configuration from a YAML file
func LoadChainsConfig(path string) (*ChainsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read chains config file: %w", err)
	}

	var config ChainsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse chains config: %w", err)
	}

	// Build lookup maps
	config.byChainID = make(map[int64]*Chain, len(config.Chains))
	for i := range config.Chains {
		chain := &config.Chains[i]
		config.byChainID[chain.ChainID] = chain
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate validates the chains configuration
func (c *ChainsConfig) Validate() error {
	if len(c.Chains) == 0 {
		return fmt.Errorf("at least one chain must be configured")
	}

	seen := make(map[int64]bool)
	for _, chain := range c.Chains {
		if chain.ChainID <= 0 {
			return fmt.Errorf("invalid chain_id for chain %s", chain.Name)
		}
		if chain.Name == "" {
			return fmt.Errorf("chain name is required for chain_id %d", chain.ChainID)
		}
		if chain.AlchemyNetwork == "" {
			return fmt.Errorf("alchemy_network is required for chain %s", chain.Name)
		}
		if chain.NativeAsset == "" {
			return fmt.Errorf("native_asset is required for chain %s", chain.Name)
		}
		if chain.NativeDecimals <= 0 {
			return fmt.Errorf("native_decimals must be positive for chain %s", chain.Name)
		}
		if chain.NativeCoinGeckoID == "" {
			return fmt.Errorf("native_coingecko_id is required for chain %s", chain.Name)
		}
		if seen[chain.ChainID] {
			return fmt.Errorf("duplicate chain_id %d", chain.ChainID)
		}
		seen[chain.ChainID] = true
	}

	return nil
}

// GetChain returns the chain configuration for a given chain ID
func (c *ChainsConfig) GetChain(chainID int64) (*Chain, bool) {
	chain, ok := c.byChainID[chainID]
	return chain, ok
}

// GetAlchemyNetwork returns the Alchemy network identifier for a chain ID
func (c *ChainsConfig) GetAlchemyNetwork(chainID int64) (string, bool) {
	chain, ok := c.byChainID[chainID]
	if !ok {
		return "", false
	}
	return chain.AlchemyNetwork, true
}

// GetNativeAsset returns the native asset symbol for a chain ID
func (c *ChainsConfig) GetNativeAsset(chainID int64) (string, bool) {
	chain, ok := c.byChainID[chainID]
	if !ok {
		return "", false
	}
	return chain.NativeAsset, true
}

// GetChainIDs returns all supported chain IDs
func (c *ChainsConfig) GetChainIDs() []int64 {
	ids := make([]int64, 0, len(c.Chains))
	for _, chain := range c.Chains {
		ids = append(ids, chain.ChainID)
	}
	return ids
}

// IsSupported checks if a chain ID is supported
func (c *ChainsConfig) IsSupported(chainID int64) bool {
	_, ok := c.byChainID[chainID]
	return ok
}
