package models

import (
	"math/big"
	"testing"
)

func TestFormatSortablePriorityPreservesNumericOrder(t *testing.T) {
	a := FormatSortablePriority(big.NewInt(9))
	b := FormatSortablePriority(big.NewInt(100))
	if a >= b {
		t.Fatalf("lexicographic order broken: %q >= %q", a, b)
	}
}
