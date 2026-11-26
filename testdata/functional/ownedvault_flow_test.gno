package ownedvault

import "testing"

func expectPanicVault(t *testing.T, label string, fn func()) {
	t.Helper()
	if r := revive(fn); r == nil {
		t.Fatalf("%s: expected panic, but did not panic", label)
	}
}

func TestOwnedVaultBaseConstructorsPreserved(cur realm, t *testing.T) {
	ownerAddr := address("g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5")
	attacker := address("g1us8428u2a5satrlxzagqqa5m6vmuze025anjlj")

	testing.SetRealm(testing.NewUserRealm(ownerAddr))
	Init(cross(cur), U256FromU64(500))

	// Base Owned constructor() debe haber fijado owner = ownerAddr
	if Owner() != ownerAddr {
		t.Fatalf("base constructor Owned lost! expected owner=%s got=%s", string(ownerAddr), string(Owner()))
	}
	// Base Tagged("vault-v1") constructor debe haber fijado tag = "vault-v1"
	if Tag() != "vault-v1" {
		t.Fatalf("base constructor Tagged lost! expected tag='vault-v1' got=%q", Tag())
	}
	if !Cap().Eq(U256FromU64(500)) {
		t.Fatalf("expected cap=500, got %s", Cap().String())
	}

	// Owner puede actualizar cap
	SetCap(cross(cur), U256FromU64(1000))
	if !Cap().Eq(U256FromU64(1000)) {
		t.Fatalf("expected cap=1000, got %s", Cap().String())
	}

	// Attacker NO puede llamar SetCap
	testing.SetRealm(testing.NewUserRealm(attacker))
	expectPanicVault(t, "attacker setCap", func() {
		SetCap(cross(cur), U256FromU64(9999))
	})
}
