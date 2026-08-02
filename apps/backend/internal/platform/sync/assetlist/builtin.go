package assetlist

import "strings"

// The generator is a standalone program carrying the `ignore` build tag, so it
// is named by file path rather than by package — `go run ./gen` would report
// that build constraints exclude every file in it.
//go:generate go run gen/main.go

// builtinKey is the (chain, contract) identity the generated list is keyed on.
// Chain slugs and contracts are stored lowercased by the generator, so lookups
// normalize the same way and a checksum-cased address still hits.
type builtinKey struct {
	Chain    string
	Contract string
}

// Lookup reports whether (chain, contract) is one of the major coins in the
// compiled-in list, returning the ticker the list carries for it.
//
// The returned symbol is NOT used to decide knownness — the contract alone
// decides that. It is returned so a caller can log the mismatch when a leg's
// ticker disagrees with the list's, which is precisely the homoglyph case: a leg
// calling itself USDC at a contract the list knows as something else is a fact
// worth seeing.
func Lookup(chain, contract string) (string, bool) {
	sym, ok := builtin[builtinKey{
		Chain:    strings.ToLower(strings.TrimSpace(chain)),
		Contract: strings.ToLower(strings.TrimSpace(contract)),
	}]
	return sym, ok
}

// Size reports how many entries the compiled-in list carries. Used by the
// startup log line, so an empty or truncated generated list is visible at boot
// rather than as unexplained missing balance days later.
func Size() int { return len(builtin) }
