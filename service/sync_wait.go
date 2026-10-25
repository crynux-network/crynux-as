package service

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTaskJobWaitTimeout = 10 * time.Minute
	ClientTimeoutBuffer       = 15 * time.Second
)

// ResolveTaskJobWaitTimeout returns how long a synchronous handler holds the
// HTTP response while waiting for a job. When the client sends a usable
// Stainless timeout header, AS waits that duration plus a short buffer.
// Otherwise AS keeps the existing 10-minute ceiling.
func ResolveTaskJobWaitTimeout(header http.Header) time.Duration {
	if seconds, ok := parsePositiveSecondsHeader(header.Get("X-Stainless-Timeout")); ok {
		return time.Duration(seconds*float64(time.Second)) + ClientTimeoutBuffer
	}
	if seconds, ok := parsePositiveSecondsHeader(header.Get("X-Stainless-Read-Timeout")); ok {
		return time.Duration(seconds*float64(time.Second)) + ClientTimeoutBuffer
	}
	return DefaultTaskJobWaitTimeout
}

func parsePositiveSecondsHeader(raw string) (float64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return seconds, true
}
