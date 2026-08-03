package reconcilereport

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Snapshot is the provider's raw balance response for a wallet, frozen on disk.
//
// It is the SECOND ENTRANCE of the same command, not an alternative to the live
// one. Three reasons, all from #41:
//
//   - The provider budget is capped at a handful of calls per session while a
//     report under development is run dozens of times.
//   - Acceptance has to be repeatable on THE SAME data, otherwise "we fixed it"
//     is indistinguishable from "something changed on chain".
//   - It makes P↔L reproducible by construction, which is what lets two runs be
//     diffed against each other — the main way this check is used.
//
// It is also the fixture the tests run on, so the tested path and the operated
// path are one path.
//
// Chains holds the provider's response VERBATIM, before this package's
// normalization. Storing normalized positions instead would freeze the very
// translation the report exists to exercise, and a snapshot taken by a buggy
// build would keep reproducing the bug as though it were data.
type Snapshot struct {
	// WalletAddress is whose balances these are, so a snapshot cannot be run
	// against the wrong wallet without saying so.
	WalletAddress string `json:"wallet_address"`

	// CapturedAt is when the positions were taken from the provider. It is one
	// half of the time gap the report PRINTS rather than pretends to close (the
	// other half is the sync cursor). It is an input, fixed at capture, so two
	// runs over one snapshot still produce byte-identical output.
	CapturedAt time.Time `json:"captured_at"`

	// Chains maps chain slug to the provider's raw balance array for it.
	Chains map[string][]RawBalance `json:"chains"`

	// NewerThanCursor names the chains where the provider held transactions
	// dated after the collection cursor at capture time.
	//
	// It is recorded IN THE SNAPSHOT rather than re-probed on replay, because
	// the answer is a property of the moment the positions were taken. Asking
	// again during a replay would compare today's chain against yesterday's
	// balances and make the snapshot stop reproducing its own verdict — the one
	// property the snapshot exists to have.
	//
	// Empty when the probe was not performed; the report says so rather than
	// reading absence as "no newer transactions".
	NewerThanCursor []string `json:"newer_than_cursor,omitempty"`

	// CursorProbed records whether the newer-than-cursor probe ran at all, so an
	// empty NewerThanCursor can be told from an unasked question.
	CursorProbed bool `json:"cursor_probed"`
}

// ChainNames returns the snapshot's chains in sorted order, so iteration over a
// map cannot leak into the report's ordering.
func (s Snapshot) ChainNames() []string {
	names := make([]string, 0, len(s.Chains))
	for c := range s.Chains {
		names = append(names, c)
	}
	sort.Strings(names)
	return names
}

// Positions normalizes every chain in the snapshot into P rows.
func (s Snapshot) Positions() ([]Position, error) {
	var out []Position
	for _, chain := range s.ChainNames() {
		ps, err := NormalizePositions(chain, s.Chains[chain])
		if err != nil {
			return nil, err
		}
		out = append(out, ps...)
	}
	return out, nil
}

// LoadSnapshot reads a snapshot from disk.
func LoadSnapshot(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot %s: %w", path, err)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to decode snapshot %s: %w", path, err)
	}
	if len(s.Chains) == 0 {
		return nil, fmt.Errorf("snapshot %s contains no chains", path)
	}
	return &s, nil
}

// SaveSnapshot writes a snapshot to disk, indented so a human can read the
// evidence the verdict was reached on.
func SaveSnapshot(path string, s *Snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode snapshot: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write snapshot %s: %w", path, err)
	}
	return nil
}
