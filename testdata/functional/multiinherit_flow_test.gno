package multiinherit

import "testing"

func TestMultiInheritC3Resolution(t *testing.T) {
	if BaseVal() != 10 {
		t.Fatalf("expected BaseVal() == 10, got %d", BaseVal())
	}
	// C3 chain: MultiInherit (+1) -> super BaseB (+5) -> super BaseA (baseVal=10) == 16
	got := Compute()
	if got != 16 {
		t.Fatalf("expected Compute() == 16 (10 + 5 + 1), got %d", got)
	}
}
