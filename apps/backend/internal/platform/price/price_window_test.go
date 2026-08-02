package price_test

import (
	"testing"
	"time"

	"github.com/kislikjeka/moontrack/internal/platform/price"
	"github.com/stretchr/testify/require"
)

func TestGranularityOf_DerivedFromTimestampAlignment(t *testing.T) {
	tests := []struct {
		name string
		at   time.Time
		want price.PricePointGranularity
	}{
		{
			name: "UTC midnight is a daily point (CoinGecko /history fallback)",
			at:   time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC),
			want: price.GranularityDaily,
		},
		{
			name: "exact hour is an hourly point (market_chart/range)",
			at:   time.Date(2025, 3, 14, 15, 0, 0, 0, time.UTC),
			want: price.GranularityHourly,
		},
		{
			name: "arbitrary sub-hour instant is a spot point (PriceUpdater)",
			at:   time.Date(2025, 3, 14, 15, 7, 42, 123456, time.UTC),
			want: price.GranularitySpot,
		},
		{
			name: "whole minute but not whole hour is still spot",
			at:   time.Date(2025, 3, 14, 15, 30, 0, 0, time.UTC),
			want: price.GranularitySpot,
		},
		{
			name: "alignment is judged in UTC, not the local zone",
			at:   time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC).In(time.FixedZone("X", 30*60)),
			want: price.GranularityDaily,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, price.GranularityOf(tc.at))
		})
	}
}

func TestToleranceWindow_MatchesThePeriodThePointSpeaksFor(t *testing.T) {
	require.Equal(t, 24*time.Hour, price.GranularityDaily.ToleranceWindow())
	require.Equal(t, time.Hour, price.GranularityHourly.ToleranceWindow())
	require.Equal(t, time.Duration(0), price.GranularitySpot.ToleranceWindow())
}

// A daily point must cover the whole day it starts, otherwise the day-granular
// CoinGecko fallback would miss its own window on every lookup and send the
// backfill worker back to the provider each time.
func TestPricePointCovers_DailyPointCoversItsWholeDay(t *testing.T) {
	midnight := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)

	require.True(t, price.PricePointCovers(midnight, midnight),
		"the day's own midnight")
	require.True(t, price.PricePointCovers(midnight, midnight.Add(23*time.Hour+59*time.Minute)),
		"end of the same day")
	require.False(t, price.PricePointCovers(midnight, midnight.Add(24*time.Hour)),
		"next day's midnight belongs to the next daily point")
}

func TestPricePointCovers_HourlyPointCoversOnlyItsHour(t *testing.T) {
	hour := time.Date(2025, 3, 14, 15, 0, 0, 0, time.UTC)

	require.True(t, price.PricePointCovers(hour, hour.Add(59*time.Minute)))
	require.False(t, price.PricePointCovers(hour, hour.Add(time.Hour)))
	require.False(t, price.PricePointCovers(hour, hour.Add(6*time.Hour)))
}

// The defect this window exists for: a spot row written by the background
// updater must never answer a question about a later transaction moment.
func TestPricePointCovers_SpotPointNeverCoversALaterMoment(t *testing.T) {
	spotWrite := time.Date(2025, 3, 13, 9, 17, 3, 500, time.UTC)

	require.False(t, price.PricePointCovers(spotWrite, spotWrite.Add(time.Minute)))
	require.False(t, price.PricePointCovers(spotWrite, spotWrite.Add(30*time.Hour)))
}

func TestPricePointCovers_PointAfterTargetNeverCovers(t *testing.T) {
	midnight := time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)
	require.False(t, price.PricePointCovers(midnight, midnight.Add(-time.Second)))
}
