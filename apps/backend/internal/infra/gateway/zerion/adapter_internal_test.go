package zerion

import (
	"math"
	"math/big"
	"testing"
)

// TestUsdFloatToBigInt covers the MT-SYNC-07 range-guarded conversion: normal
// prices scale by 1e8, large-but-valid prices convert without int64 overflow,
// and non-finite / negative / over-ceiling prices are rejected as nil.
func TestUsdFloatToBigInt(t *testing.T) {
	tests := []struct {
		name  string
		price float64
		want  *big.Int // nil means "expect nil"
	}{
		{
			name:  "normal price scales by 1e8",
			price: 3500.12,
			want:  big.NewInt(350012000000),
		},
		{
			name:  "zero price",
			price: 0,
			want:  big.NewInt(0),
		},
		{
			// ~$500 billion / unit: overflows the old int64 path (max ~$9.2e10),
			// but must convert cleanly via big.Float. 5e11 * 1e8 = 5e19.
			name:  "large valid price does not overflow",
			price: 5e11,
			want:  func() *big.Int { n, _ := new(big.Int).SetString("50000000000000000000", 10); return n }(),
		},
		{
			name:  "at the ceiling is accepted",
			price: maxUSDPrice, // 1e12 * 1e8 = 1e20
			want:  func() *big.Int { n, _ := new(big.Int).SetString("100000000000000000000", 10); return n }(),
		},
		{
			name:  "above ceiling rejected",
			price: maxUSDPrice * 2,
			want:  nil,
		},
		{
			name:  "NaN rejected",
			price: math.NaN(),
			want:  nil,
		},
		{
			name:  "positive infinity rejected",
			price: math.Inf(1),
			want:  nil,
		},
		{
			name:  "negative infinity rejected",
			price: math.Inf(-1),
			want:  nil,
		},
		{
			name:  "negative price rejected",
			price: -1.5,
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := usdFloatToBigInt(tt.price)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("usdFloatToBigInt(%v) = %s, want nil", tt.price, got.String())
				}
				return
			}
			if got == nil {
				t.Fatalf("usdFloatToBigInt(%v) = nil, want %s", tt.price, tt.want.String())
			}
			if got.Cmp(tt.want) != 0 {
				t.Fatalf("usdFloatToBigInt(%v) = %s, want %s", tt.price, got.String(), tt.want.String())
			}
		})
	}
}
