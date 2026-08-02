package sync

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// NativeContract is the contract sentinel carried by a chain's native coin
// (issue #56, decision #35). Uniqueness is held by the chain, so (ethereum,
// native), (base, native) and (arbitrum, native) are three distinct assets.
//
// It is a LITERAL, deliberately not a pseudo-address such as 0xeeee…eeee. A
// pseudo-address looks like a valid address, so a native leg mistakenly handled
// as a token would clear every shape check and resolve to a key — the silent
// failure this whole change exists to remove. `native` is visibly wrong in a
// log line, a SELECT and a debugger at the first glance.
//
// It is also not a companion is_native flag: a flag is a second source of truth
// about nativeness that nothing keeps consistent with the contract column.
//
// The literal replaces four incompatible spellings of "native" that predate it:
// the empty string emitted by the provider adapter, the empty-string column
// default in chain_assets, NULL in Asset.IsNativeL1(), and the hardcoded "ETH"
// fallback in the transfer handlers that yields account codes like
// `gas.polygon.ETH`.
const NativeContract = "native"

// AssetKey is the on-chain identity of an asset: the chain it lives on plus its
// contract, with the native coin carrying NativeContract. It is the key of the
// asset registry and the argument of every resolve.
//
// Symbol is NOT part of it. Two contracts sharing a ticker on one chain are two
// assets — the case that motivated the registry — and one coin present on two
// chains is likewise two assets, an accepted property of the composite key
// rather than a defect to paper over.
type AssetKey struct {
	Chain    string
	Contract string
}

// NewAssetKey builds a normalized AssetKey. Normalization is applied here, at
// the one place keys are minted, so a caller cannot construct a key that misses
// the registry because of casing or stray whitespace.
//
// EVM addresses are case-insensitive, and providers are inconsistent about
// checksum casing, so the contract is lowercased. Lowercasing the sentinel is a
// no-op, which keeps the rule single-branched.
func NewAssetKey(chain, contract string) AssetKey {
	contract = strings.ToLower(strings.TrimSpace(contract))
	if contract == "" {
		contract = NativeContract
	}
	return AssetKey{
		Chain:    strings.ToLower(strings.TrimSpace(chain)),
		Contract: contract,
	}
}

// IsNative reports whether the key names a chain's native coin.
func (k AssetKey) IsNative() bool {
	return k.Contract == NativeContract
}

// Valid reports whether both halves of the identity are present. The registry
// enforces the same rule with NOT NULL plus a non-blank CHECK; this is the
// in-process guard so a blank key is refused before it reaches the database.
func (k AssetKey) Valid() bool {
	return k.Chain != "" && k.Contract != ""
}

// String renders the key as chain:contract, for log lines and errors.
func (k AssetKey) String() string {
	return k.Chain + ":" + k.Contract
}

// RegistryAsset is a row of the asset registry: a stable UUID plus the metadata
// hanging off an on-chain identity.
type RegistryAsset struct {
	ID       uuid.UUID
	Key      AssetKey
	Symbol   string
	Name     string
	Decimals int
}

// AssetRegistry resolves an on-chain identity to the stable UUID that names the
// asset, creating the registry row on first sight.
//
// The resolve happens in SYNC, before anything enters the ledger, because the
// ledger knows nothing about blockchains — not addresses, and not chains as a
// source of identity. Resolving inside the ledger would require an arrow
// against the layering rule transport → module → platform → ledger.
type AssetRegistry interface {
	// Resolve returns the registry asset for the key, inserting it when absent.
	// It is idempotent on the key: concurrent first-sights of the same asset
	// converge on one row and one UUID.
	//
	// Metadata (symbol, name, decimals) is used only when the row is created.
	// A later sighting never overwrites it, so a provider that reports a
	// different decimals for an already-known contract cannot corrupt the
	// base-unit conversions already performed against the stored value — the
	// exact failure mode that (symbol, chain) uniqueness produces today in
	// chain_assets.
	Resolve(ctx context.Context, key AssetKey, symbol, name string, decimals int) (*RegistryAsset, error)
}
