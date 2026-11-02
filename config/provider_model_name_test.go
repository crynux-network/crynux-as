package config

import "testing"

func TestNormalizeProviderModelName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"  GPT-4o  ", "gpt-4o"},
		{"gpt-4o", "gpt-4o"},
		{"Claude-Opus-5-5", "claude-opus-5-5"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := NormalizeProviderModelName(tc.in); got != tc.want {
			t.Fatalf("NormalizeProviderModelName(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}
