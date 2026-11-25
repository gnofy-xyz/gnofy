package simpleerc20

import (
	"strings"
	"testing"

	"gno.land/p/nt/testutils/v0"
)

func expectPanicGRC20(t *testing.T, label string, fn func()) {
	t.Helper()
	if r := revive(fn); r == nil {
		t.Fatalf("%s: expected panic, but did not panic", label)
	}
}

func TestSimpleERC20IdiomaticGRC20Flow(cur realm, t *testing.T) {
	ownerAddr := testutils.TestAddress("owner")
	alice := testutils.TestAddress("alice")
	bob := testutils.TestAddress("bob")

	testing.SetRealm(testing.NewUserRealm(ownerAddr))
	Init(cross(cur), "GnofyToken", "GNOF", 1000)

	if Name() != "GnofyToken" || Symbol() != "GNOF" || Decimals() != 18 {
		t.Fatalf("metadata mismatch: %s / %s / %d", Name(), Symbol(), Decimals())
	}
	if TotalSupply() != 1000 || BalanceOf(ownerAddr) != 1000 {
		t.Fatalf("initial supply/balance wrong: supply=%d owner=%d", TotalSupply(), BalanceOf(ownerAddr))
	}

	Transfer(cross(cur), alice, 300)
	if BalanceOf(ownerAddr) != 700 || BalanceOf(alice) != 300 {
		t.Fatalf("transfer balances wrong: owner=%d alice=%d", BalanceOf(ownerAddr), BalanceOf(alice))
	}

	testing.SetRealm(testing.NewUserRealm(alice))
	Approve(cross(cur), bob, 150)
	if Allowance(alice, bob) != 150 {
		t.Fatalf("allowance wrong: %d", Allowance(alice, bob))
	}

	testing.SetRealm(testing.NewUserRealm(bob))
	TransferFrom(cross(cur), alice, bob, 100)
	if BalanceOf(alice) != 200 || BalanceOf(bob) != 100 || Allowance(alice, bob) != 50 {
		t.Fatalf("transferFrom wrong: alice=%d bob=%d allow=%d", BalanceOf(alice), BalanceOf(bob), Allowance(alice, bob))
	}

	// Mint: owner OK, alice panic
	testing.SetRealm(testing.NewUserRealm(ownerAddr))
	Mint(cross(cur), bob, 500)
	if TotalSupply() != 1500 || BalanceOf(bob) != 600 {
		t.Fatalf("mint wrong: supply=%d bob=%d", TotalSupply(), BalanceOf(bob))
	}

	testing.SetRealm(testing.NewUserRealm(alice))
	expectPanicGRC20(t, "non-owner mint", func() {
		Mint(cross(cur), alice, 10)
	})

	// TransferOwnership vía gno.land/p/nt/ownable/v0
	testing.SetRealm(testing.NewUserRealm(ownerAddr))
	TransferOwnership(cross(cur), alice)
	if Owner() != alice {
		t.Fatalf("expected new owner %s, got %s", string(alice), string(Owner()))
	}

	// Render() nativo de GRC-20
	rendered := Render("")
	if !strings.Contains(rendered, "GnofyToken") || !strings.Contains(rendered, "GNOF") {
		t.Fatalf("unexpected RenderHome output: %s", rendered)
	}
}
