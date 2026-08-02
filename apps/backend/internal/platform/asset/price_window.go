package asset

import "time"

// Historical price lookup answers "what was this asset worth at moment T?".
// price_history holds two very different kinds of rows for that question:
//
//   - Historical points, written by the price backfill pipeline for a specific
//     past moment. Their timestamp is normalized to the granularity the
//     provider could actually deliver: UTC midnight for the day-granular
//     CoinGecko /history fallback, the exact hour for an hourly range query.
//   - Spot points, written by the background PriceUpdater at time.Now(). They
//     describe "right now", not any particular past moment, and land on an
//     arbitrary sub-second timestamp.
//
// A nearest-row-at-or-before lookup with no lower bound cannot tell these
// apart, so a lot whose transaction moment is *later* than the last spot write
// silently receives an arbitrarily stale spot price presented as exact. The
// tolerance window below is the lower bound that stops it.
//
// The window width is derived from the granularity of the point that actually
// landed, read off the row's own timestamp alignment — not from a separate
// constant. That matters because the fallback path degrades to daily points: a
// fixed hourly window would reject every daily point as a miss and send the
// worker back to the provider on every single lookup.

// PricePointGranularity is the resolution a stored price point claims, derived
// from how its timestamp is aligned.
type PricePointGranularity int

const (
	// GranularitySpot means the point is not aligned to any period boundary.
	// It came from the background updater and describes "now" — it is never a
	// valid answer to a question about a past moment.
	GranularitySpot PricePointGranularity = iota
	// GranularityHourly means the point sits exactly on an hour boundary; it
	// speaks for the hour that starts there.
	GranularityHourly
	// GranularityDaily means the point sits exactly on UTC midnight; it speaks
	// for the day that starts there.
	GranularityDaily
)

// GranularityOf classifies a stored point by its timestamp alignment.
// Midnight UTC → daily, exact hour → hourly, anything else → spot.
func GranularityOf(pointTime time.Time) PricePointGranularity {
	utc := pointTime.UTC()
	if utc.Nanosecond() != 0 || utc.Second() != 0 || utc.Minute() != 0 {
		return GranularitySpot
	}
	if utc.Hour() == 0 {
		return GranularityDaily
	}
	return GranularityHourly
}

// ToleranceWindow is how far after a stored point that point may still be used
// as the answer. It equals the period the point speaks for. A spot point
// speaks for no past period at all, so its window is zero.
func (g PricePointGranularity) ToleranceWindow() time.Duration {
	switch g {
	case GranularityDaily:
		return 24 * time.Hour
	case GranularityHourly:
		return time.Hour
	default:
		return 0
	}
}

// PricePointCovers reports whether a point stored at pointTime may answer a
// lookup for target. The point must not be in the future relative to target,
// and target must fall inside the window the point's own granularity implies.
func PricePointCovers(pointTime, target time.Time) bool {
	if pointTime.After(target) {
		return false
	}
	window := GranularityOf(pointTime).ToleranceWindow()
	if window == 0 {
		// Spot point — only an exact hit is honest, and an exact hit against a
		// sub-second spot timestamp does not happen in practice.
		return pointTime.Equal(target)
	}
	return target.Sub(pointTime) < window
}
