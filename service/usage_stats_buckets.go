package service

import (
	"fmt"
	"math"
)

// DurationBucket describes one predefined completion-duration small bucket.
type DurationBucket struct {
	ID            string
	MinDurationMs uint64
	MaxDurationMs *uint64 // nil means no upper bound
	Label         string
}

var durationBuckets = []DurationBucket{
	{ID: "0_250", MinDurationMs: 0, MaxDurationMs: uint64Ptr(250), Label: "0-250ms"},
	{ID: "250_500", MinDurationMs: 250, MaxDurationMs: uint64Ptr(500), Label: "250-500ms"},
	{ID: "500_1000", MinDurationMs: 500, MaxDurationMs: uint64Ptr(1000), Label: "500ms-1s"},
	{ID: "1000_2000", MinDurationMs: 1000, MaxDurationMs: uint64Ptr(2000), Label: "1-2s"},
	{ID: "2000_5000", MinDurationMs: 2000, MaxDurationMs: uint64Ptr(5000), Label: "2-5s"},
	{ID: "5000_10000", MinDurationMs: 5000, MaxDurationMs: uint64Ptr(10000), Label: "5-10s"},
	{ID: "10000_30000", MinDurationMs: 10000, MaxDurationMs: uint64Ptr(30000), Label: "10-30s"},
	{ID: "30000_60000", MinDurationMs: 30000, MaxDurationMs: uint64Ptr(60000), Label: "30-60s"},
	{ID: "60000_120000", MinDurationMs: 60000, MaxDurationMs: uint64Ptr(120000), Label: "1-2min"},
	{ID: "120000_300000", MinDurationMs: 120000, MaxDurationMs: uint64Ptr(300000), Label: "2-5min"},
	{ID: "300000_600000", MinDurationMs: 300000, MaxDurationMs: uint64Ptr(600000), Label: "5-10min"},
	{ID: "600000_inf", MinDurationMs: 600000, MaxDurationMs: nil, Label: "10min+"},
}

func DurationBuckets() []DurationBucket {
	out := make([]DurationBucket, len(durationBuckets))
	copy(out, durationBuckets)
	return out
}

func DurationBucketID(durationMs uint64) string {
	for _, bucket := range durationBuckets {
		if bucket.MaxDurationMs == nil {
			if durationMs >= bucket.MinDurationMs {
				return bucket.ID
			}
			continue
		}
		if durationMs >= bucket.MinDurationMs && durationMs < *bucket.MaxDurationMs {
			return bucket.ID
		}
	}
	return durationBuckets[len(durationBuckets)-1].ID
}

func DurationBucketByID(id string) (DurationBucket, bool) {
	for _, bucket := range durationBuckets {
		if bucket.ID == id {
			return bucket, true
		}
	}
	return DurationBucket{}, false
}

type DisplayDurationBucket struct {
	BucketIndex   uint
	BucketLabel   string
	MinDurationMs uint64
	MaxDurationMs *uint64
	RequestCount  uint64
}

type durationBucketCount struct {
	bucket DurationBucket
	count  uint64
}

// MergeDurationDisplayBuckets merges adjacent small buckets into at most 12 display buckets.
func MergeDurationDisplayBuckets(counts map[string]uint64) []DisplayDurationBucket {
	ordered := make([]durationBucketCount, 0, len(durationBuckets))
	var total uint64
	for _, bucket := range durationBuckets {
		count := counts[bucket.ID]
		total += count
		ordered = append(ordered, durationBucketCount{bucket: bucket, count: count})
	}
	if total == 0 {
		return nil
	}

	const maxDisplayBuckets = 12
	threshold := uint64(math.Ceil(float64(total) / float64(maxDisplayBuckets)))
	if threshold == 0 {
		threshold = 1
	}

	var result []DisplayDurationBucket
	start := 0
	for start < len(ordered) {
		if len(result) == maxDisplayBuckets-1 {
			result = append(result, newDisplayDurationBucket(uint(len(result)), ordered[start:]))
			break
		}
		end := start
		var sum uint64
		for end < len(ordered) {
			sum += ordered[end].count
			end++
			if sum >= threshold {
				break
			}
		}
		result = append(result, newDisplayDurationBucket(uint(len(result)), ordered[start:end]))
		start = end
	}
	return result
}

func newDisplayDurationBucket(index uint, parts []durationBucketCount) DisplayDurationBucket {
	first := parts[0].bucket
	last := parts[len(parts)-1].bucket
	var count uint64
	for _, part := range parts {
		count += part.count
	}
	label := first.Label
	if first.ID != last.ID {
		if last.MaxDurationMs == nil {
			label = fmt.Sprintf("%s+", formatDurationBound(first.MinDurationMs))
		} else {
			label = fmt.Sprintf("%s-%s", formatDurationBound(first.MinDurationMs), formatDurationBound(*last.MaxDurationMs))
		}
	}
	return DisplayDurationBucket{
		BucketIndex:   index,
		BucketLabel:   label,
		MinDurationMs: first.MinDurationMs,
		MaxDurationMs: cloneUint64Ptr(last.MaxDurationMs),
		RequestCount:  count,
	}
}

func formatDurationBound(ms uint64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60000:
		if ms%1000 == 0 {
			return fmt.Sprintf("%ds", ms/1000)
		}
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	default:
		if ms%60000 == 0 {
			return fmt.Sprintf("%dmin", ms/60000)
		}
		return fmt.Sprintf("%.1fmin", float64(ms)/60000)
	}
}

func uint64Ptr(v uint64) *uint64 {
	return &v
}

func cloneUint64Ptr(v *uint64) *uint64 {
	if v == nil {
		return nil
	}
	copied := *v
	return &copied
}

// HourStartUnix returns the Unix second at the start of the hour containing ts.
func HourStartUnix(ts int64) int64 {
	return ts - (ts % 3600)
}

// TenMinuteStartUnix returns the Unix second at the start of the 10-minute period containing ts.
func TenMinuteStartUnix(ts int64) int64 {
	return ts - (ts % 600)
}

// UnixDayStart returns floor(ts / 86400) * 86400.
func UnixDayStart(ts int64) int64 {
	return (ts / 86400) * 86400
}
