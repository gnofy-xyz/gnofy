// Package passes implementa las transformaciones deterministas sobre la IR
// (C3 flattening, desugaring de modifiers, mapeo de tipos, crossing y estándares).
package passes

import (
	"fmt"

	"github.com/gnofy-xyz/gnofy/internal/ir"
)

// Pass transforma una Module IR de forma determinista.
type Pass interface {
	Name() string
	Run(mod *ir.Module) error
}

// Pipeline ejecuta una lista ordenada de pases sobre la IR.
type Pipeline struct {
	passes []Pass
}

// NewPipeline crea un pipeline con los pases indicados en orden de ejecución.
func NewPipeline(passes ...Pass) *Pipeline {
	return &Pipeline{passes: passes}
}

// DefaultPipeline devuelve la secuencia estándar de pases deterministas de gnofy.
func DefaultPipeline() *Pipeline {
	return NewPipeline(
		C3FlattenPass{},
		ModifiersPass{},
		CrossingPass{},
		NamingPass{},
	)
}

// IdiomaticPipeline añade StandardsPass para emitir integraciones nativas de gno.land
// (GRC-20, ownable, Render Markdown enriquecido).
func IdiomaticPipeline() *Pipeline {
	return NewPipeline(
		C3FlattenPass{},
		ModifiersPass{},
		CrossingPass{},
		NamingPass{},
		StandardsPass{},
	)
}

// Run ejecuta cada pase secuencialmente y se detiene ante el primer error.
func (p *Pipeline) Run(mod *ir.Module) error {
	for _, pass := range p.passes {
		if err := pass.Run(mod); err != nil {
			return fmt.Errorf("pass %s: %w", pass.Name(), err)
		}
	}
	return nil
}
