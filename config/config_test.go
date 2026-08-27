package config

import "testing"

func TestParseEmptyQueueMedianPriorityGweiPreservesLargeInteger(t *testing.T) {
	cfg := &AppConfig{}
	cfg.LLM.EmptyQueueMedianPriorityGwei = "20000000000"

	got, err := cfg.ParseEmptyQueueMedianPriorityGwei()
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "20000000000" {
		t.Fatalf("median priority = %s, want 20000000000", got)
	}
}

func TestParseEmptyQueueMedianPriorityGweiRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "+1", "1.5", " 1"} {
		cfg := &AppConfig{}
		cfg.LLM.EmptyQueueMedianPriorityGwei = value
		if _, err := cfg.ParseEmptyQueueMedianPriorityGwei(); err == nil {
			t.Errorf("median priority %q was accepted", value)
		}
	}
}
