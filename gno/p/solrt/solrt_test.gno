package solrt

import "testing"

func TestU256Arithmetic(t *testing.T) {
	a := U256FromString("1000000000000000000000000") // 1e24
	b := U256FromString("250000000000000000000000")  // 2.5e23

	sum := a.Add(b)
	if sum.String() != "1250000000000000000000000" {
		t.Fatalf("Add mismatch: got %s", sum.String())
	}

	diff := a.Sub(b)
	if diff.String() != "750000000000000000000000" {
		t.Fatalf("Sub mismatch: got %s", diff.String())
	}

	prod := U256FromU64(1000000000000).Mul(U256FromU64(1000000000000))
	if prod.String() != "1000000000000000000000000" {
		t.Fatalf("Mul mismatch: got %s", prod.String())
	}

	// MulDiv sin overflow intermedio (x*y > 2^256)
	big := U256FromU64(10).Pow(40)
	md := MulDiv(big, big, big)
	if !md.Eq(big) {
		t.Fatalf("MulDiv mismatch: got %s want %s", md.String(), big.String())
	}
}

func TestOrderedMap(t *testing.T) {
	m := NewOrderedMap()
	if !m.GetU256("missing").IsZero() {
		t.Fatalf("expected zero value for missing key")
	}

	m.SetU256("alice", U256FromU64(500))
	m.SetU256("bob", U256FromU64(250))
	m.SetU256("charlie", U256FromU64(100))
	m.SetU256("alice", U256FromU64(750))

	if m.Size() != 3 {
		t.Fatalf("expected size 3, got %d", m.Size())
	}
	if m.GetU256("alice").Uint64() != 750 {
		t.Fatalf("expected alice=750, got %s", m.GetU256("alice").String())
	}
}

func expectSolrtPanic(t *testing.T, label string, fn func()) {
	t.Helper()
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		fn()
	}()
	if !panicked {
		t.Fatalf("%s: expected panic", label)
	}
}

func TestExactWidthOverflowChecks(t *testing.T) {
	if SafeAddU8(200, 55) != 255 {
		t.Fatalf("SafeAddU8(200, 55) != 255")
	}
	expectSolrtPanic(t, "uint8 overflow", func() { SafeAddU8(255, 1) })
	expectSolrtPanic(t, "uint16 overflow", func() { SafeAddU16(65535, 1) })
	expectSolrtPanic(t, "uint32 overflow", func() { SafeAddU32(4294967295, 1) })

	// uint128 límite exacto: 2^128 - 1 OK, +1 panic
	maxU128 := U256FromU64(1).Pow(0)
	_ = maxU128
	two128 := U256FromU64(2).Pow(128)
	two128Minus1 := two128.Sub(U256FromU64(1))
	two128Minus1.CheckBits(128) // OK
	expectSolrtPanic(t, "uint128 overflow", func() { two128.CheckBits(128) })

	// Signed checked arithmetic
	if SafeSubI64(10, 25) != -15 {
		t.Fatalf("SafeSubI64(10, 25) != -15")
	}
	expectSolrtPanic(t, "int8 overflow", func() { SafeAddI8(127, 1) })
	expectSolrtPanic(t, "int8 underflow", func() { SafeSubI8(-128, 1) })
	expectSolrtPanic(t, "int64 min/-1 div overflow", func() { SafeDivI64(minInt64, -1) })
	expectSolrtPanic(t, "int64 min neg overflow", func() { SafeNegI64(minInt64) })
}
