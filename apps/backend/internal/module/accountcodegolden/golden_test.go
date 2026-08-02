package accountcodegolden

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// update rewrites the golden file instead of comparing against it:
//
//	go test ./internal/module/accountcodegolden/ -update
//
// Regenerating is only correct when the account-code shape was changed on
// purpose. During the centralisation of the code constructor (#55) the golden
// file must stay byte-for-byte identical, so -update must not be used there.
var update = flag.Bool("update", false, "rewrite the account code golden file")

const goldenPath = "testdata/account_codes.golden"

// requiredPrefixes are the seven account-code namespaces the ledger knows.
// The corpus is required to reach every one of them: a namespace missing from
// the emitted set means the golden file silently stopped guarding that branch,
// which is exactly the "silence is indistinguishable from success" failure the
// golden file exists to prevent.
var requiredPrefixes = []string{
	"wallet.",
	"income.",
	"expense.",
	"gas.",
	"clearing.",
	"collateral.",
	"liability.",
}

// requiredCodes are code shapes that a prefix check alone would not pin down.
// The income. namespace has four qualified variants that each come from a
// different handler, and collateral./liability. carry a five-segment form
// (namespace, protocol, wallet, chain, asset) that a bare prefix match would
// not distinguish from the four-segment wallet form.
var requiredCodes = []struct {
	name  string
	match func(code string) bool
}{
	{"income.genesis. variant", hasPrefix("income.genesis.")},
	{"income.lp. variant", hasPrefix("income.lp.")},
	{"income.defi. variant", hasPrefix("income.defi.")},
	{"income.lending. variant", hasPrefix("income.lending.")},
	{"five-segment collateral.", segmentedPrefix("collateral.", 5)},
	{"five-segment liability.", segmentedPrefix("liability.", 5)},
}

func hasPrefix(prefix string) func(string) bool {
	return func(code string) bool { return strings.HasPrefix(code, prefix) }
}

// segmentedPrefix matches codes under prefix that have exactly n dot-separated
// segments.
func segmentedPrefix(prefix string, n int) func(string) bool {
	return func(code string) bool {
		return strings.HasPrefix(code, prefix) && len(strings.Split(code, ".")) == n
	}
}

// TestAccountCodesGolden runs every transaction handler over the corpus and
// compares the sorted set of emitted account codes against the golden file.
//
// It is a shape test, not a behaviour test: it deliberately ignores amounts,
// entry types and metadata, and asserts only on the strings that end up in
// Entry.Metadata["account_code"].
func TestAccountCodesGolden(t *testing.T) {
	codes := collectCodes(t)

	if len(codes) == 0 {
		t.Fatal("corpus produced no account codes at all")
	}

	// A handler that failed above leaves the set incomplete, which would turn
	// into a cascade of misleading "namespace missing" and golden-diff errors.
	// The subtest failure is the real finding; stop here.
	if t.Failed() {
		t.Fatal("a corpus case failed: the emitted set is incomplete, " +
			"skipping coverage and golden comparison")
	}

	assertPrefixCoverage(t, codes)
	assertRequiredCodes(t, codes)

	got := strings.Join(codes, "\n") + "\n"

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create testdata dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("golden file rewritten with %d codes", len(codes))
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file (run with -update to create it): %v", err)
	}

	if want := string(wantBytes); got != want {
		t.Errorf("account code set differs from %s\n%s", goldenPath, diff(want, got))
	}
}

// collectCodes drives every corpus case and returns the sorted, deduplicated
// set of account codes.
func collectCodes(t *testing.T) []string {
	t.Helper()

	// context.Background() carries no user ID, which makes the wallet
	// ownership check in ValidateData a no-op. That is intentional: the
	// corpus is about code shape, not authorization.
	ctx := context.Background()

	// Each case is a subtest so that one broken handler does not hide the
	// state of the other fifteen: a failing case reports and the loop moves on.
	set := make(map[string]struct{})
	for _, c := range corpus() {
		t.Run(c.name, func(t *testing.T) {
			entries, err := c.handler.Handle(ctx, c.data)
			if err != nil {
				t.Fatalf("%s: Handle failed: %v", c.handler.Type(), err)
			}
			if len(entries) == 0 {
				t.Fatalf("%s: produced no entries", c.handler.Type())
			}

			for i, e := range entries {
				code, ok := e.Metadata["account_code"].(string)
				if !ok || code == "" {
					t.Fatalf("%s: entry %d has no account_code in metadata",
						c.handler.Type(), i)
				}
				set[code] = struct{}{}
			}
		})
	}

	codes := make([]string, 0, len(set))
	for code := range set {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// assertPrefixCoverage fails when the corpus stopped reaching a namespace.
func assertPrefixCoverage(t *testing.T, codes []string) {
	t.Helper()

	for _, prefix := range requiredPrefixes {
		found := false
		for _, code := range codes {
			if strings.HasPrefix(code, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("namespace %q is not represented in the emitted set: "+
				"either a handler stopped producing it, or the corpus no longer "+
				"reaches it. Add a case to corpus() that exercises it — do not "+
				"drop the requirement.", prefix)
		}
	}
}

// assertRequiredCodes fails when a qualified code shape is missing.
func assertRequiredCodes(t *testing.T, codes []string) {
	t.Helper()

	for _, req := range requiredCodes {
		found := false
		for _, code := range codes {
			if req.match(code) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no code matching %s in the emitted set: the corpus no "+
				"longer covers this variant", req.name)
		}
	}
}

// diff renders a line-level comparison of two newline-separated code sets.
func diff(want, got string) string {
	wantLines := strings.Split(strings.TrimSuffix(want, "\n"), "\n")
	gotLines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")

	inWant := make(map[string]struct{}, len(wantLines))
	for _, l := range wantLines {
		inWant[l] = struct{}{}
	}
	inGot := make(map[string]struct{}, len(gotLines))
	for _, l := range gotLines {
		inGot[l] = struct{}{}
	}

	var b strings.Builder
	for _, l := range wantLines {
		if _, ok := inGot[l]; !ok {
			b.WriteString("  - " + l + "  (in golden, not emitted)\n")
		}
	}
	for _, l := range gotLines {
		if _, ok := inWant[l]; !ok {
			b.WriteString("  + " + l + "  (emitted, not in golden)\n")
		}
	}
	if b.Len() == 0 {
		return "  (same lines, different order or trailing bytes)\n"
	}
	return b.String()
}
