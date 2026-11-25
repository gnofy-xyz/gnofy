package counter

import "testing"

func expectPanic(t *testing.T, label string, fn func()) {
	t.Helper()
	if r := revive(fn); r == nil {
		t.Fatalf("%s: expected panic, but did not panic", label)
	}
}

func TestCounterFullFlow(cur realm, t *testing.T) {
	alice := address("g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5")
	testing.SetRealm(testing.NewUserRealm(alice))

	if Count() != 0 || Get() != 0 {
		t.Fatalf("expected initial count 0, got %d", Count())
	}

	Increment(cross(cur))
	Increment(cross(cur))
	if Count() != 2 || Get() != 2 {
		t.Fatalf("expected count 2 after 2 increments, got %d", Count())
	}

	Decrement(cross(cur))
	Decrement(cross(cur))
	if Count() != 0 {
		t.Fatalf("expected count 0 after 2 decrements, got %d", Count())
	}

	expectPanic(t, "decrement underflow", func() {
		Decrement(cross(cur))
	})
}
