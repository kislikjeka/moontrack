//go:build ignore

// Command gen builds the compiled-in major-coin list for the known-asset filter
// (#58) from PUBLIC TOKEN LISTS. It is run by `go generate ./internal/platform/sync/assetlist`
// (see generate.go) and writes builtin_gen.go.
//
// The list is generated, never hand-edited. A hand-maintained allow-list rots
// silently: nobody notices a missing entry until a real token stops entering the
// ledger, and by then the omission looks like a bug somewhere else. Generation
// makes the list a build artifact with a recorded provenance, and regenerating
// it is a diff a reviewer can read.
//
// WHAT THIS LIST IS FOR, AND WHAT IT IS NOT FOR. It answers "is this one of the
// major coins", cheaply and offline. It is emphatically NOT the verifier of
// whether an asset is legitimate — decision #37 measured that and rejected it:
// token lists cut 4 of 5 real DeFi positions, because a debt token or an LP
// share is not a coin and will never appear in any list, yet it must still be
// valued. That is what level 2 (quotability) exists for. This list only has to
// cover the bulk cheaply; anything it misses falls through to level 2.
//
// The `ignore` build tag at the top keeps this file out of the normal build: it
// is a program, not part of the package it writes into.
package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// source is one public token list, plus how its chain ids map onto MoonTrack's
// chain slugs.
type source struct {
	Name string
	URL  string
}

// sources are the public token lists the built-in list is distilled from.
//
// Uniswap's is the canonical multi-chain list; CoinGecko's per-chain lists fill
// in coverage on the chains MoonTrack actually syncs. Both are served as the
// standard Token List schema, so one decoder handles them.
var sources = []source{
	{Name: "uniswap", URL: "https://tokens.uniswap.org"},
	{Name: "coingecko-ethereum", URL: "https://tokens.coingecko.com/ethereum/all.json"},
	{Name: "coingecko-base", URL: "https://tokens.coingecko.com/base/all.json"},
	{Name: "coingecko-arbitrum", URL: "https://tokens.coingecko.com/arbitrum-one/all.json"},
}

// chainIDToSlug maps EVM numeric chain ids (what the Token List schema carries)
// to MoonTrack's chain slugs. A token on a chain not listed here is dropped:
// the built-in list may only speak about chains the sync layer knows, otherwise
// it would grant knownness on a chain whose legs can never be reconciled.
var chainIDToSlug = map[int]string{
	1:     "ethereum",
	56:    "binance-smart-chain",
	137:   "polygon",
	8453:  "base",
	42161: "arbitrum",
	10:    "optimism",
	43114: "avalanche",
}

type tokenListEntry struct {
	ChainID  int    `json:"chainId"`
	Address  string `json:"address"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Decimals int    `json:"decimals"`
}

type tokenList struct {
	Name   string           `json:"name"`
	Tokens []tokenListEntry `json:"tokens"`
}

type entry struct {
	Chain    string
	Contract string
	Symbol   string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "assetlist/gen:", err)
		os.Exit(1)
	}
}

func run() error {
	client := &http.Client{Timeout: 60 * time.Second}

	// Keyed on (chain, contract) — the same identity the filter resolves on, so
	// the same contract appearing in several lists collapses to one entry.
	seen := make(map[[2]string]entry)
	provenance := make([]string, 0, len(sources))

	for _, s := range sources {
		list, err := fetch(client, s.URL)
		if err != nil {
			// A failed fetch must fail the generation. Silently emitting a
			// shorter list would quietly shrink what may enter the ledger, and
			// the resulting gap would surface much later as missing balance.
			return fmt.Errorf("fetch %s (%s): %w", s.Name, s.URL, err)
		}

		kept := 0
		for _, t := range list.Tokens {
			slug, ok := chainIDToSlug[t.ChainID]
			if !ok {
				continue
			}
			addr := strings.ToLower(strings.TrimSpace(t.Address))
			if addr == "" || t.Symbol == "" {
				continue
			}
			key := [2]string{slug, addr}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = entry{Chain: slug, Contract: addr, Symbol: t.Symbol}
			kept++
		}
		provenance = append(provenance, fmt.Sprintf("//   %-20s %s (%d entries on synced chains)", s.Name, s.URL, kept))
	}

	if len(seen) == 0 {
		return fmt.Errorf("every source yielded zero entries — refusing to write an empty list")
	}

	entries := make([]entry, 0, len(seen))
	for _, e := range seen {
		entries = append(entries, e)
	}
	// Deterministic order, so regenerating without an upstream change produces
	// a byte-identical file and the diff shows only real movement.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Chain != entries[j].Chain {
			return entries[i].Chain < entries[j].Chain
		}
		return entries[i].Contract < entries[j].Contract
	})

	src, err := render(entries, provenance)
	if err != nil {
		return err
	}
	return os.WriteFile("builtin_gen.go", src, 0o644)
}

func fetch(client *http.Client, url string) (*tokenList, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	var list tokenList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

func render(entries []entry, provenance []string) ([]byte, error) {
	var b strings.Builder

	b.WriteString("// Code generated by internal/platform/sync/assetlist/gen. DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString("// Regenerate with:  go generate ./internal/platform/sync/assetlist\n")
	b.WriteString("//\n")
	b.WriteString("// Distilled from these public token lists:\n")
	for _, p := range provenance {
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("//\n")
	b.WriteString("// This list answers \"is this one of the major coins\", nothing more. It is NOT\n")
	b.WriteString("// a legitimacy verifier: a debt token or an LP share is not a coin and appears\n")
	b.WriteString("// in no list, yet must still be valued. Those are caught by level 2 of the\n")
	b.WriteString("// resolve (quotability), which is why a miss here is harmless.\n\n")
	b.WriteString("package assetlist\n\n")
	b.WriteString("// builtin maps (chain, contract) to the ticker the list carries for it.\n")
	b.WriteString("// The symbol is metadata, never identity — two contracts sharing a ticker on\n")
	b.WriteString("// one chain are two different assets, which is the case the whole registry\n")
	b.WriteString("// exists to keep apart.\n")
	b.WriteString("var builtin = map[builtinKey]string{\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "\t{%q, %q}: %q,\n", e.Chain, e.Contract, e.Symbol)
	}
	b.WriteString("}\n")

	return format.Source([]byte(b.String()))
}
