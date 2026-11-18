package passes

import (
	"github.com/gnofy-xyz/gnofy/internal/ir"
)

// ModifiersPass expande deterministamente los modifiers de Solidity dentro del cuerpo
// de cada función (pre-condiciones antes del cuerpo y post-condiciones después).
type ModifiersPass struct{}

func (ModifiersPass) Name() string { return "modifiers" }

func (ModifiersPass) Run(mod *ir.Module) error {
	for _, c := range mod.Contracts {
		modByName := make(map[string]*ir.ModifierDef, len(c.Modifiers))
		for _, m := range c.Modifiers {
			modByName[m.Name] = m
		}

		for _, fn := range c.Functions {
			if len(fn.Modifiers) == 0 {
				continue
			}
			body := fn.Body
			// Aplicar modifiers de derecha a izquierda para respetar orden de ejecución Solidity
			for i := len(fn.Modifiers) - 1; i >= 0; i-- {
				mc := fn.Modifiers[i]
				if mc.Name == "nonReentrant" {
					c.UsesReentrancyGuard = true
					c.UsesSolrt = true
					// El generador emitirá el guard reentrancyLocked + defer
					continue
				}
				def, ok := modByName[mc.Name]
				if !ok {
					continue
				}
				body = expandModifier(def, body)
			}
			fn.Body = body
		}
	}
	return nil
}

func expandModifier(def *ir.ModifierDef, inner []ir.Stmt) []ir.Stmt {
	var out []ir.Stmt
	replaced := false
	for _, s := range def.Body {
		if _, isPH := s.(*ir.PlaceholderStmt); isPH {
			out = append(out, inner...)
			replaced = true
			continue
		}
		out = append(out, s)
	}
	if !replaced {
		out = append(out, inner...)
	}
	return out
}
