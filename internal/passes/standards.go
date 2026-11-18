package passes

import (
	"strconv"

	"github.com/gnofy-xyz/gnofy/internal/ir"
)

// StandardsPass detecta estándares Solidity (ERC-20, Ownable) para emitir
// integraciones nativas de gno.land (gno.land/p/nt/grc20/v0, gno.land/p/nt/ownable/v0)
// cuando el usuario activa --idiomatic.
type StandardsPass struct{}

func (StandardsPass) Name() string { return "standards" }

func (StandardsPass) Run(mod *ir.Module) error {
	for _, c := range mod.Contracts {
		if c.Kind != ir.KindContract || c.Abstract {
			continue
		}

		fnNames := make(map[string]bool)
		for _, fn := range c.Functions {
			if fn.Name != "" {
				fnNames[fn.Name] = true
			}
		}
		varNames := make(map[string]*ir.StateVar)
		for _, sv := range c.StateVars {
			varNames[sv.Name] = sv
			if sv.Visibility == ir.VisPublic {
				fnNames[sv.Name] = true
			}
		}

		// Detectar ERC-20 estándar (transfer + approve + transferFrom + balanceOf + totalSupply)
		isERC20 := fnNames["transfer"] &&
			fnNames["approve"] &&
			fnNames["transferFrom"] &&
			fnNames["balanceOf"] &&
			fnNames["totalSupply"] &&
			len(c.Holes) == 0

		if isERC20 {
			c.IdiomaticGRC20 = true
			c.HasMint = fnNames["mint"]
			c.HasBurn = fnNames["burn"]
			c.DecimalsDefault = 6 // valor idiomático habitual en Gno/GRC-20 si no se declara
			if decVar, ok := varNames["decimals"]; ok && decVar.InitialValue != nil {
				if lit, ok := decVar.InitialValue.(*ir.LiteralExpr); ok {
					if v, err := strconv.Atoi(lit.Value); err == nil && v >= 0 && v <= 18 {
						c.DecimalsDefault = v
					}
				}
			}
		}

		// Detectar patrón Ownable (base Ownable o variable owner + modifier onlyOwner/require_owner)
		hasOwnerBase := false
		for _, b := range c.BaseNames {
			if b == "Ownable" {
				hasOwnerBase = true
			}
		}
		hasOwnerMod := false
		for _, m := range c.Modifiers {
			if m.Name == "onlyOwner" || m.Name == "require_owner" {
				hasOwnerMod = true
			}
		}
		if hasOwnerBase || (varNames["owner"] != nil && hasOwnerMod) {
			c.IdiomaticOwnable = true
		}
	}
	return nil
}
