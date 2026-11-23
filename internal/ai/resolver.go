// Package ai define la capa opcional de resolución quirúrgica de nodos HoleIR
// y el lockfile (gnofy.lock) que garantiza reproducibilidad en CI.
package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/gnofy-xyz/gnofy/internal/ir"
)

// HoleRequest encapsula el contexto mínimo y acotado enviado al motor IA para un hueco aislado.
type HoleRequest struct {
	Hole              *ir.Hole
	ContractName      string
	FunctionSignature string
	IsCrossing        bool
	StateVarsSummary  string
	AllowedImports    []string
	PreviousError     string // solo relleno en reintentos tras fallo de verificación
}

// ResolutionSource indica de dónde provino el código que rellena un hueco.
type ResolutionSource string

const (
	SourceStub   ResolutionSource = "stub"
	SourceLock   ResolutionSource = "lock"
	SourceAIFresh ResolutionSource = "ai"
)

// HoleResolution contiene el bloque Gno sugerido para reemplazar un HoleIR.
type HoleResolution struct {
	GnoCode string
	Model   string
	Source  ResolutionSource
	Cached  bool
}

// HoleResolver resuelve nodos HoleIR de forma opcional y acotada.
type HoleResolver interface {
	Resolve(ctx context.Context, req HoleRequest) (HoleResolution, error)
}

// HoleResolverFunc adapta una función a la interfaz HoleResolver.
type HoleResolverFunc func(ctx context.Context, req HoleRequest) (HoleResolution, error)

func (f HoleResolverFunc) Resolve(ctx context.Context, req HoleRequest) (HoleResolution, error) {
	return f(ctx, req)
}

// NoopResolver es la implementación por defecto (--ai=off): 100% determinista.
type NoopResolver struct{}

// Resolve emite un stub panic seguro documentando el motivo del hueco.
func (NoopResolver) Resolve(_ context.Context, req HoleRequest) (HoleResolution, error) {
	code := fmt.Sprintf("panic(%q) // TODO(gnofy): revisar hueco %s", req.Hole.Reason, req.Hole.ID)
	return HoleResolution{
		GnoCode: code,
		Model:   "deterministic-stub",
		Source:  SourceStub,
		Cached:  true,
	}, nil
}

// FormatAnnotated envuelve una resolución IA con delimitadores auditables para grep/revisión.
func FormatAnnotated(holeID string, res HoleResolution) string {
	if res.Source == SourceStub {
		return res.GnoCode
	}
	var b strings.Builder
	fmt.Fprintf(&b, "// BEGIN gnofy-ai hole=%s model=%s source=%s\n", holeID, res.Model, res.Source)
	b.WriteString(strings.TrimSpace(res.GnoCode))
	b.WriteByte('\n')
	fmt.Fprintf(&b, "// END gnofy-ai hole=%s", holeID)
	return b.String()
}
