package tokenvault

import (
	"strings"
	"testing"
)

type mockVaultERC20 struct {
	caller     address
	supply     U256
	balances   *OrderedMap
	allowances *OrderedMap
}

func newMockVaultERC20() *mockVaultERC20 {
	return &mockVaultERC20{
		balances:   NewOrderedMap(),
		allowances: NewOrderedMap(),
	}
}

func (m *mockVaultERC20) setCaller(a address) { m.caller = a }

func (m *mockVaultERC20) mint(to address, amt U256) {
	m.supply = m.supply.Add(amt)
	m.balances.SetU256(string(to), m.balances.GetU256(string(to)).Add(amt))
}

func (m *mockVaultERC20) approve(owner, spender address, amt U256) {
	m.allowances.SetU256(KeyJoin(string(owner), string(spender)), amt)
}

func (m *mockVaultERC20) TotalSupply() U256 { return m.supply }

func (m *mockVaultERC20) BalanceOf(account address) U256 {
	return m.balances.GetU256(string(account))
}

func (m *mockVaultERC20) Allowance(owner, spender address) U256 {
	return m.allowances.GetU256(KeyJoin(string(owner), string(spender)))
}

func (m *mockVaultERC20) Transfer(to address, amount U256) bool {
	from := m.caller
	bal := m.balances.GetU256(string(from))
	if bal.Lt(amount) {
		return false
	}
	m.balances.SetU256(string(from), bal.Sub(amount))
	m.balances.SetU256(string(to), m.balances.GetU256(string(to)).Add(amount))
	return true
}

func (m *mockVaultERC20) TransferFrom(from, to address, amount U256) bool {
	bal := m.balances.GetU256(string(from))
	if bal.Lt(amount) {
		return false
	}
	m.balances.SetU256(string(from), bal.Sub(amount))
	m.balances.SetU256(string(to), m.balances.GetU256(string(to)).Add(amount))
	return true
}

func TestTokenVaultFullLifecycle(cur realm, t *testing.T) {
	ownerAddr := address("g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5")
	alice := address("g125em6arxsnj49vx3egfu0sv5tuy5puc0um6y2q")
	bob := address("g1us8428u2a5satrlxzagqqa5m6vmuze025anjlj")
	treasury := address("g1treasury000000000000000000000000000001")
	tokAddr := address("g1token000000000000000000000000000000001")

	deployer = ownerAddr
	tok := newMockVaultERC20()
	RegisterERC20(tokAddr, tok)

	var realmAddr address
	func(cur realm) {
		realmAddr = cur.Address()
	}(cross(cur))

	// 1. Init & OpenVault
	testing.SetRealm(testing.NewUserRealm(ownerAddr))
	Init(cross(cur), tokAddr, U256FromU64(900000))
	OpenVault(cross(cur))
	if Status() != VaultStatus_OPEN {
		t.Fatalf("expected VaultStatus_OPEN, got %d", Status())
	}

	// 2. Alice deposits 500,000 with 1000 bps (10%) fee -> fee=50,000, net=450,000
	tok.mint(alice, U256FromU64(500000))
	tok.approve(alice, realmAddr, U256FromU64(500000))
	testing.SetRealm(testing.NewUserRealm(alice))
	Deposit(cross(cur), U256FromU64(1000))

	// 3. Bob deposits 500,000 with 1000 bps (10%) fee -> totalNet=900,000 -> auto-close!
	tok.mint(bob, U256FromU64(500000))
	tok.approve(bob, realmAddr, U256FromU64(500000))
	testing.SetRealm(testing.NewUserRealm(bob))
	Deposit(cross(cur), U256FromU64(1000))

	if Status() != VaultStatus_CLOSED {
		t.Fatalf("expected auto-close VaultStatus_CLOSED, got %d", Status())
	}
	if !TotalNet().Eq(U256FromU64(900000)) || !TotalFees().Eq(U256FromU64(100000)) {
		t.Fatalf("totals mismatch: net=%s fees=%s", TotalNet().String(), TotalFees().String())
	}

	pos := GetPosition(alice)
	if !pos.RawAmount.Eq(U256FromU64(500000)) || !pos.NetAmount.Eq(U256FromU64(450000)) {
		t.Fatalf("alice position mismatch: raw=%s net=%s", pos.RawAmount.String(), pos.NetAmount.String())
	}

	// 4. CreateOrder & CancelOrder (tests storage struct ref + storage slice swap-and-pop)
	testing.SetRealm(testing.NewUserRealm(alice))
	CreateOrder(cross(cur), U256FromU64(1), U256FromU64(100000))
	if len(orderIds) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orderIds))
	}
	CancelOrder(cross(cur), U256FromU64(1))
	if len(orderIds) != 0 {
		t.Fatalf("expected 0 orders after pop(), got %d", len(orderIds))
	}

	// 5. WithdrawAll as owner -> treasury receives full 1,000,000
	tok.setCaller(realmAddr)
	testing.SetRealm(testing.NewUserRealm(ownerAddr))
	WithdrawAll(cross(cur), treasury)
	if !tok.BalanceOf(treasury).Eq(U256FromU64(1000000)) {
		t.Fatalf("expected treasury=1000000, got %s", tok.BalanceOf(treasury).String())
	}

	if !strings.Contains(Render(""), "TokenVault") {
		t.Fatalf("Render() missing TokenVault header")
	}
}
