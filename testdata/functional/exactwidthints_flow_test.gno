package exactwidthints

import "testing"

func expectPanicInt(t *testing.T, label string, fn func()) {
	t.Helper()
	panicked := false
	if r := revive(func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		fn()
	}); r != nil {
		panicked = true
	}
	if !panicked {
		t.Fatalf("%s: expected panic, but did not panic", label)
	}
}

func TestExactWidthIntsCheckedSemantics(cur realm, t *testing.T) {
	alice := address("g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5")
	testing.SetRealm(testing.NewUserRealm(alice))

	// uint8: 250 + 5 = 255 OK, +1 panic
	if AddSmall(cross(cur), 5) != 255 {
		t.Fatalf("expected small=255, got %d", Small())
	}
	expectPanicInt(t, "uint8 inc overflow", func() {
		IncSmall(cross(cur))
	})

	// uint16: 65530 + 5 = 65535 OK, +1 panic
	if AddMed(cross(cur), 5) != 65535 {
		t.Fatalf("expected med=65535, got %d", Med())
	}
	expectPanicInt(t, "uint16 overflow", func() {
		AddMed(cross(cur), 1)
	})

	// uint32: 4294967290 + 5 = 4294967295 OK, +1 panic
	if AddBig32(cross(cur), 5) != 4294967295 {
		t.Fatalf("expected big32=4294967295, got %d", Big32())
	}
	expectPanicInt(t, "uint32 overflow", func() {
		AddBig32(cross(cur), 1)
	})

	// uint128: (2^128 - 2) + 1 = 2^128 - 1 OK, (2^128 - 1) + 1 panic!
	maxU128 := U256FromU64(2).Pow(128).Sub(U256FromU64(1))
	maxU128Minus1 := maxU128.Sub(U256FromU64(1))
	got128 := SetAndAdd128(cross(cur), maxU128Minus1, U256FromU64(1))
	if !got128.Eq(maxU128) {
		t.Fatalf("expected maxU128, got %s", got128.String())
	}
	expectPanicInt(t, "uint128 overflow at 2^128", func() {
		SetAndAdd128(cross(cur), maxU128, U256FromU64(1))
	})

	// int8: 120 + 7 = 127 OK, +1 panic!
	if AddSigned8(cross(cur), 7) != 127 {
		t.Fatalf("expected signed8=127, got %d", Signed8())
	}
	expectPanicInt(t, "int8 overflow", func() {
		AddSigned8(cross(cur), 1)
	})

	// int64: -10 - 5 = -15 OK; MinInt64 / -1 panic!
	if SubSigned64(cross(cur), 5) != -15 {
		t.Fatalf("expected signed64=-15, got %d", Signed64())
	}
	expectPanicInt(t, "int64 MinInt / -1 overflow", func() {
		DivSigned64(-9223372036854775808, -1)
	})
}
