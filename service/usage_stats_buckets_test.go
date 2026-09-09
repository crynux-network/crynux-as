package service

import (
	"testing"
)

func TestHourAndTenMinuteAndDayStarts(t *testing.T) {
	if got := HourStartUnix(1_700_001_234); got != 1_699_999_200 {
		t.Fatalf("HourStartUnix = %d", got)
	}
	if got := TenMinuteStartUnix(1_700_001_234); got != 1_700_001_000 {
		t.Fatalf("TenMinuteStartUnix = %d", got)
	}
	if got := UnixDayStart(1_700_001_234); got != 1_699_920_000 {
		t.Fatalf("UnixDayStart = %d", got)
	}
}

func TestDurationBucketID(t *testing.T) {
	cases := []struct {
		ms   uint64
		want string
	}{
		{0, "0_250"},
		{249, "0_250"},
		{250, "250_500"},
		{999, "500_1000"},
		{600000, "600000_inf"},
		{900000, "600000_inf"},
	}
	for _, tc := range cases {
		if got := DurationBucketID(tc.ms); got != tc.want {
			t.Fatalf("DurationBucketID(%d)=%s want %s", tc.ms, got, tc.want)
		}
	}
}

func TestMergeDurationDisplayBuckets(t *testing.T) {
	counts := map[string]uint64{
		"0_250":      10,
		"250_500":    10,
		"500_1000":   10,
		"1000_2000":  10,
		"2000_5000":  10,
		"5000_10000": 10,
		"10000_30000": 10,
		"30000_60000": 10,
		"60000_120000": 10,
		"120000_300000": 10,
		"300000_600000": 10,
		"600000_inf": 10,
	}
	display := MergeDurationDisplayBuckets(counts)
	if len(display) == 0 || len(display) > 12 {
		t.Fatalf("unexpected display bucket count: %d", len(display))
	}
	var sum uint64
	for _, bucket := range display {
		sum += bucket.RequestCount
	}
	if sum != 120 {
		t.Fatalf("sum=%d want 120", sum)
	}

	empty := MergeDurationDisplayBuckets(map[string]uint64{})
	if empty != nil {
		t.Fatalf("empty counts must return nil")
	}
}

func TestMergeDurationDisplayBucketsDoesNotSplitSmallBuckets(t *testing.T) {
	counts := map[string]uint64{
		"0_250": 100,
		"250_500": 1,
	}
	display := MergeDurationDisplayBuckets(counts)
	if len(display) < 1 {
		t.Fatal("expected display buckets")
	}
	found := false
	for _, bucket := range display {
		if bucket.MinDurationMs == 0 && bucket.MaxDurationMs != nil && *bucket.MaxDurationMs == 250 {
			if bucket.RequestCount != 100 {
				t.Fatalf("small bucket split unexpectedly: %d", bucket.RequestCount)
			}
			found = true
		}
	}
	if !found {
		// 100 vs threshold ceil(101/12)=9, so first bucket closes after 0_250 alone
		if display[0].RequestCount != 100 {
			t.Fatalf("first display bucket=%d want 100", display[0].RequestCount)
		}
	}
}

func TestAccountHourlyPeriodStarts1d(t *testing.T) {
	now := int64(1_700_000_100) // within an hour starting at 1700000000? 1700000100 - 1700000100%3600
	starts, complete, err := accountHourlyPeriodStarts(AccountStatsRange1d, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(starts) != 24 {
		t.Fatalf("len=%d", len(starts))
	}
	current := HourStartUnix(now)
	if starts[len(starts)-1] != current {
		t.Fatalf("last start=%d want %d", starts[len(starts)-1], current)
	}
	if complete[len(complete)-1] {
		t.Fatal("current hour must be incomplete")
	}
	if !complete[0] {
		t.Fatal("oldest hour must be complete")
	}
}

func TestDailyExpandedIncludesCurrentDayHoursOnlyUpToNow(t *testing.T) {
	currentHour := HourStartUnix(1_700_003_600)
	starts, complete, err := dailyExpandedHourStarts(currentHour, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(starts) == 0 {
		t.Fatal("expected starts")
	}
	if starts[len(starts)-1] != currentHour {
		t.Fatalf("last=%d want %d", starts[len(starts)-1], currentHour)
	}
	if complete[len(complete)-1] {
		t.Fatal("current hour incomplete")
	}
	for i := 0; i < len(starts)-1; i++ {
		if !complete[i] {
			t.Fatalf("hour %d should be complete", starts[i])
		}
	}
}
