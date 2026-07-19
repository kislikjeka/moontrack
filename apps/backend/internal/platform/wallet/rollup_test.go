package wallet

import "testing"

func chainRow(status SyncStatus) WalletChainSync {
	return WalletChainSync{SyncStatus: status}
}

func TestRollupStatus(t *testing.T) {
	tests := []struct {
		name string
		rows []WalletChainSync
		want SyncStatus
	}{
		{
			name: "empty set rolls up to pending",
			rows: nil,
			want: SyncStatusPending,
		},
		{
			name: "all synced -> synced",
			rows: []WalletChainSync{chainRow(SyncStatusSynced), chainRow(SyncStatusSynced)},
			want: SyncStatusSynced,
		},
		{
			name: "all pending -> pending",
			rows: []WalletChainSync{chainRow(SyncStatusPending), chainRow(SyncStatusPending)},
			want: SyncStatusPending,
		},
		{
			name: "any error dominates syncing and synced",
			rows: []WalletChainSync{chainRow(SyncStatusSynced), chainRow(SyncStatusSyncing), chainRow(SyncStatusError)},
			want: SyncStatusError,
		},
		{
			name: "any syncing (no error) -> syncing even with a synced chain",
			rows: []WalletChainSync{chainRow(SyncStatusSynced), chainRow(SyncStatusSyncing)},
			want: SyncStatusSyncing,
		},
		{
			name: "synced + pending (no syncing/error) -> synced",
			rows: []WalletChainSync{chainRow(SyncStatusSynced), chainRow(SyncStatusPending)},
			want: SyncStatusSynced,
		},
		{
			name: "error beats a later pending chain",
			rows: []WalletChainSync{chainRow(SyncStatusError), chainRow(SyncStatusPending)},
			want: SyncStatusError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RollupStatus(tt.rows); got != tt.want {
				t.Fatalf("RollupStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
