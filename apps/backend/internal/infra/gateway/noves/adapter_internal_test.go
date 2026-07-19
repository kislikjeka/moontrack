package noves

import "testing"

func TestDomainToNovesChain(t *testing.T) {
	tests := []struct {
		domain string
		want   string
		ok     bool
	}{
		{"ethereum", "eth", true},
		{"binance-smart-chain", "bsc", true},
		{"base", "base", true},
		{"arbitrum", "arbitrum", true},
		{"polygon", "polygon", true},
		{"optimism", "optimism", true},
		{"avalanche", "avalanche", true},
		{"solana", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := domainToNovesChain(tt.domain)
		if ok != tt.ok || got != tt.want {
			t.Errorf("domainToNovesChain(%q) = (%q, %v), want (%q, %v)", tt.domain, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNovesToDomainChain_Roundtrip(t *testing.T) {
	for domain := range domainToNoves {
		noves, ok := domainToNovesChain(domain)
		if !ok {
			t.Fatalf("domainToNovesChain(%q) not ok", domain)
		}
		back, ok := novesToDomainChain(noves)
		if !ok {
			t.Fatalf("novesToDomainChain(%q) not ok", noves)
		}
		if back != domain {
			t.Errorf("roundtrip %q -> %q -> %q", domain, noves, back)
		}
	}
	// ethereum specifically must roundtrip through the short slug.
	if noves, _ := domainToNovesChain("ethereum"); noves != "eth" {
		t.Fatalf("ethereum should map to eth, got %q", noves)
	}
	if back, _ := novesToDomainChain("eth"); back != "ethereum" {
		t.Fatalf("eth should map back to ethereum, got %q", back)
	}
}

func TestIsNativeAddress(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"ETH", true},
		{"", true},
		{"MATIC", true},
		{"0x833589fcd6edb6e08f4c7c32d4f71b54bda02913", false},
		{"0xCBB7C0000AB88B473B1F5AFD9EF808440EED33BF", false},
	}
	for _, tt := range tests {
		if got := isNativeAddress(tt.address); got != tt.want {
			t.Errorf("isNativeAddress(%q) = %v, want %v", tt.address, got, tt.want)
		}
	}
}

func TestNormalizeContract(t *testing.T) {
	if got := normalizeContract("ETH"); got != "" {
		t.Errorf("native token contract should be empty, got %q", got)
	}
	if got := normalizeContract("0x833589FCD6EDB6E08F4C7C32D4F71B54BDA02913"); got != "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913" {
		t.Errorf("contract should be lowercased, got %q", got)
	}
}

func TestFractionalDigits(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"120", 0},
		{"120.5", 1},
		{"1.23456789", 8},
		{"0.000002387988852403", 18},
		{"1.", 0},
	}
	for _, tt := range tests {
		if got := fractionalDigits(tt.in); got != tt.want {
			t.Errorf("fractionalDigits(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestAmountToBaseUnits_ExactOrFlag(t *testing.T) {
	// Exact: 6 decimals, 6 fractional digits — no flag.
	amount, review := amountToBaseUnits("120.559701", 6, "USDC")
	if review != "" {
		t.Errorf("exact conversion should not flag, got %q", review)
	}
	if amount.String() != "120559701" {
		t.Errorf("got %s, want 120559701", amount.String())
	}

	// Loss: 6 decimals, 8 fractional digits — must flag but still return value.
	amount, review = amountToBaseUnits("1.23456789", 6, "USDC")
	if review == "" {
		t.Error("truncating conversion must flag with a review reason")
	}
	if amount.String() != "1234567" {
		t.Errorf("truncated amount = %s, want 1234567", amount.String())
	}

	// decimals=0 identity — no flag, no panic.
	amount, review = amountToBaseUnits("42", 0, "FOO")
	if review != "" {
		t.Errorf("decimals=0 integer should not flag, got %q", review)
	}
	if amount.String() != "42" {
		t.Errorf("got %s, want 42", amount.String())
	}
}

func TestMapOperationType(t *testing.T) {
	tests := []struct {
		novesType string
		want      string
	}{
		{"swap", "trade"},
		{"depositCollateral", "deposit"},
		{"addLiquidity", "deposit"},
		{"removeLiquidity", "withdraw"},
		{"withdrawCollateral", "withdraw"},
		// claimRewards maps to receive: the classifier's claim paths (LP fee
		// claim, lending reward claim) fire on OpReceive + a "claim" act.
		{"claimRewards", "receive"},
		{"receiveToken", "receive"},
		{"receiveFromBridge", "receive"},
		{"sendToken", "send"},
		{"sendToBridge", "send"},
		{"approveToken", "approve"},
		{"unclassified", "execute"},
		{"someUnknownFutureType", "execute"},
	}
	for _, tt := range tests {
		if got := string(mapOperationType(tt.novesType)); got != tt.want {
			t.Errorf("mapOperationType(%q) = %q, want %q", tt.novesType, got, tt.want)
		}
	}
}
