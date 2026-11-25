package simpleerc20

import "testing"

func expectPanicERC20(t *testing.T, label string, fn func()) {
	t.Helper()
	if r := revive(fn); r == nil {
		t.Fatalf("%s: expected panic, but did not panic", label)
	}
}

func TestSimpleERC20StrictFlow(cur realm, t *testing.T) {
	ownerAddr := address("g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5")
	alice := address("g1us8428u2a5satrlxzagqqa5m6vmuze025anjlj")
	bob := address("g125em6arxsnj49vx3egfu0sv5tuy5puc0um6y2q")

	// 1. Init como owner
	testing.SetRealm(testing.NewUserRealm(ownerAddr))
	Init(cross(cur), "GnofyToken", "GNOF", U256FromU64(1000))

	if Name() != "GnofyToken" || Symbol() != "GNOF" || Decimals() != 18 {
		t.Fatalf("metadata mismatch: %s / %s / %d", Name(), Symbol(), Decimals())
	}
	if !TotalSupply().Eq(U256FromU64(1000)) || !BalanceOf(ownerAddr).Eq(U256FromU64(1000)) {
		t.Fatalf("initial supply/balance mismatch: supply=%s ownerBal=%s", TotalSupply().String(), BalanceOf(ownerAddr).String())
	}
	if Owner() != ownerAddr {
		t.Fatalf("owner mismatch: got %s want %s", string(Owner()), string(ownerAddr))
	}

	// Re-init debe hacer panic
	expectPanicERC20(t, "double init", func() {
		Init(cross(cur), "X", "X", U256FromU64(1))
	})

	// 2. Transfer owner -> alice (300)
	if !Transfer(cross(cur), alice, U256FromU64(300)) {
		t.Fatalf("Transfer returned false")
	}
	if !BalanceOf(ownerAddr).Eq(U256FromU64(700)) || !BalanceOf(alice).Eq(U256FromU64(300)) {
		t.Fatalf("post-transfer balances wrong: owner=%s alice=%s", BalanceOf(ownerAddr).String(), BalanceOf(alice).String())
	}

	// 3. Approve alice -> bob (150) y TransferFrom bob (100)
	testing.SetRealm(testing.NewUserRealm(alice))
	if !Approve(cross(cur), bob, U256FromU64(150)) {
		t.Fatalf("Approve returned false")
	}
	if !Allowance(alice, bob).Eq(U256FromU64(150)) {
		t.Fatalf("allowance mismatch: got %s", Allowance(alice, bob).String())
	}

	testing.SetRealm(testing.NewUserRealm(bob))
	if !TransferFrom(cross(cur), alice, bob, U256FromU64(100)) {
		t.Fatalf("TransferFrom returned false")
	}
	if !BalanceOf(alice).Eq(U256FromU64(200)) || !BalanceOf(bob).Eq(U256FromU64(100)) {
		t.Fatalf("post-transferFrom balances wrong: alice=%s bob=%s", BalanceOf(alice).String(), BalanceOf(bob).String())
	}
	if !Allowance(alice, bob).Eq(U256FromU64(50)) {
		t.Fatalf("remaining allowance wrong: got %s want 50", Allowance(alice, bob).String())
	}

	// 4. Mint solo owner (owner OK, alice panic)
	testing.SetRealm(testing.NewUserRealm(ownerAddr))
	Mint(cross(cur), bob, U256FromU64(500))
	if !TotalSupply().Eq(U256FromU64(1500)) || !BalanceOf(bob).Eq(U256FromU64(600)) {
		t.Fatalf("post-mint mismatch: supply=%s bob=%s", TotalSupply().String(), BalanceOf(bob).String())
	}

	testing.SetRealm(testing.NewUserRealm(alice))
	expectPanicERC20(t, "non-owner mint", func() {
		Mint(cross(cur), alice, U256FromU64(1))
	})

	// 5. Transfer con saldo insuficiente debe hacer panic
	testing.SetRealm(testing.NewUserRealm(bob))
	expectPanicERC20(t, "insufficient balance", func() {
		Transfer(cross(cur), alice, U256FromU64(99999))
	})
}
