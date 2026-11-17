package solrt

// ERC20Token abstrae un token ERC-20 / GRC-20 para contratos migrados desde Solidity
// que realizan despacho dinámico de tokens por dirección (IERC20(addr), SafeERC20.*).
type ERC20Token interface {
	TotalSupply() U256
	BalanceOf(account address) U256
	Allowance(owner, spender address) U256
	Transfer(to address, amount U256) bool
	TransferFrom(from, to address, amount U256) bool
}

var erc20Registry = NewOrderedMap()

// RegisterERC20 registra una implementación de ERC20Token bajo una dirección lógica
// (útil para conectar adaptadores GRC-20 o mocks en gno test).
func RegisterERC20(addr address, token ERC20Token) {
	erc20Registry.Set(string(addr), token)
}

// ResolveERC20 resuelve el token registrado para addr (panic si no fue registrado).
func ResolveERC20(addr address) ERC20Token {
	v, ok := erc20Registry.Get(string(addr))
	if !ok || v == nil {
		panic("solrt: ERC20 token not registered for address " + string(addr))
	}
	return v.(ERC20Token)
}

// SafeERC20Transfer equivale a SafeERC20.safeTransfer(token, to, amount).
func SafeERC20Transfer(token ERC20Token, to address, amount U256) {
	Require(token.Transfer(to, amount), "SafeERC20: transfer failed")
}

// SafeERC20TransferFrom equivale a SafeERC20.safeTransferFrom(token, from, to, amount).
func SafeERC20TransferFrom(token ERC20Token, from, to address, amount U256) {
	Require(token.TransferFrom(from, to, amount), "SafeERC20: transferFrom failed")
}
