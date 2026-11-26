// Package analyzer evalúa contratos Solidity a partir del AST de solc
// y clasifica su migrabilidad determinista hacia Gno sin generar código todavía.
package analyzer

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
)

// Level clasifica el nivel de soporte determinista de un constructo Solidity.
type Level string

const (
	LevelAuto Level = "auto"
	LevelWarn Level = "warn"
	LevelHole Level = "hole"
)

// Finding documenta un patrón relevante detectado durante el análisis.
type Finding struct {
	Level      Level  `json:"level"`
	Code       string `json:"code"`
	Contract   string `json:"contract,omitempty"`
	Function   string `json:"function,omitempty"`
	Message    string `json:"message"`
	Location   string `json:"location"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ContractSummary resume las métricas de migrabilidad de un contrato concreto.
type ContractSummary struct {
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	File             string   `json:"file"`
	Abstract         bool     `json:"abstract,omitempty"`
	Bases            []string `json:"bases,omitempty"`
	StateVars        int      `json:"state_vars"`
	Structs          int      `json:"structs"`
	Enums            int      `json:"enums"`
	Modifiers        int      `json:"modifiers"`
	Events           int      `json:"events"`
	TotalFunctions   int      `json:"total_functions"`
	CleanFunctions   int      `json:"clean_functions"`
	HoleFunctions    int      `json:"hole_functions"`
	WarnCount        int      `json:"warn_count"`
	HoleCount        int      `json:"hole_count"`
	DeterministicPct float64  `json:"deterministic_pct"`
	RecommendedMode  string   `json:"recommended_mode"`
}

// Report resume el resultado global del análisis sobre uno o más archivos Solidity.
type Report struct {
	Compiler          string            `json:"compiler"`
	Files             []string          `json:"files"`
	Contracts         []ContractSummary `json:"contracts"`
	DetectedStandards []string          `json:"detected_standards,omitempty"`
	ShimmedImports    []string          `json:"shimmed_imports,omitempty"`
	TotalFunctions    int               `json:"total_functions"`
	CleanFunctions    int               `json:"clean_functions"`
	HoleFunctions     int               `json:"hole_functions"`
	WarnCount         int               `json:"warn_count"`
	HoleCount         int               `json:"hole_count"`
	DeterministicPct  float64           `json:"deterministic_pct"`
	Findings          []Finding         `json:"findings"`
}

// Analyze recorre los ASTs de los archivos objetivo y genera un Report completo.
func Analyze(res *solc.CompileResult) *Report {
	rep := &Report{
		Compiler:       res.CompilerBinary,
		Files:          slices.Clone(res.TargetFiles),
		ShimmedImports: slices.Clone(res.ShimmedImports),
	}

	standards := make(map[string]struct{})

	// Registrar advertencias a nivel de compilación (pragmas relajados / shims)
	for _, f := range res.TargetFiles {
		if orig, ok := res.RelaxedPragmas[f]; ok {
			rep.addFinding(Finding{
				Level:      LevelWarn,
				Code:       "PRAGMA_RELAXED",
				Message:    fmt.Sprintf("Pragma fijado (%s) relajado temporalmente a >=0.8.0 para analizar con el solc instalado", orig),
				Location:   f,
				Suggestion: "Instala la versión exacta de solc o usa --strict-pragma para forzar coincidencia exacta.",
			})
		}
	}

	for _, imp := range res.ShimmedImports {
		rep.addFinding(Finding{
			Level:      LevelWarn,
			Code:       "SHIM_IMPORT_USED",
			Message:    fmt.Sprintf("Import externo resuelto mediante shim embebido: %s", imp),
			Location:   imp,
			Suggestion: "Pasa --include-path <node_modules|lib> si deseas resolver la implementación real en disco.",
		})
		detectImportStandard(imp, standards)
	}

	for _, filePath := range res.TargetFiles {
		srcOut, ok := res.Sources[filePath]
		if !ok || srcOut.AST == nil || srcOut.IsShim {
			continue
		}
		analyzeSourceUnit(srcOut, rep, standards)
	}

	for s := range standards {
		rep.DetectedStandards = append(rep.DetectedStandards, s)
	}
	slices.Sort(rep.DetectedStandards)

	for _, c := range rep.Contracts {
		rep.TotalFunctions += c.TotalFunctions
		rep.CleanFunctions += c.CleanFunctions
		rep.HoleFunctions += c.HoleFunctions
	}

	if rep.TotalFunctions > 0 {
		rep.DeterministicPct = (float64(rep.CleanFunctions) / float64(rep.TotalFunctions)) * 100.0
	} else if rep.HoleCount == 0 {
		rep.DeterministicPct = 100.0
	}

	return rep
}

func (r *Report) addFinding(f Finding) {
	r.Findings = append(r.Findings, f)
	switch f.Level {
	case LevelWarn:
		r.WarnCount++
	case LevelHole:
		r.HoleCount++
	}
}

func analyzeSourceUnit(src *solc.SourceOutput, rep *Report, standards map[string]struct{}) {
	topLevelEvents := 0

	for _, node := range src.AST.Nodes {
		switch node.NodeType {
		case "ImportDirective":
			detectImportStandard(node.File, standards)
		case "EventDefinition":
			topLevelEvents++
		case "ContractDefinition":
			summary := analyzeContract(src, node, topLevelEvents, rep, standards)
			rep.Contracts = append(rep.Contracts, summary)
		}
	}
}

func analyzeContract(src *solc.SourceOutput, contract *solc.Node, topLevelEvents int, rep *Report, standards map[string]struct{}) ContractSummary {
	summary := ContractSummary{
		Name:     contract.Name,
		Kind:     contract.ContractKind,
		File:     src.Path,
		Abstract: contract.Abstract,
		Events:   topLevelEvents,
	}

	for _, base := range contract.BaseContracts {
		if base.BaseName != nil && base.BaseName.Name != "" {
			summary.Bases = append(summary.Bases, base.BaseName.Name)
			detectBaseStandard(base.BaseName.Name, standards)
		}
	}

	fnNames := make(map[string]bool)
	var warnedDynamicERC20 bool

	for _, member := range contract.Nodes {
		switch member.NodeType {
		case "VariableDeclaration":
			summary.StateVars++
			if member.Visibility == "public" && member.Name != "" {
				fnNames[member.Name] = true
			}
			analyzeVariableDecl(src, contract.Name, member, &summary, rep)
		case "StructDefinition":
			summary.Structs++
		case "EnumDefinition":
			summary.Enums++
		case "ModifierDefinition":
			summary.Modifiers++
		case "EventDefinition":
			summary.Events++
		case "FunctionDefinition":
			if !member.Implemented && contract.ContractKind == "interface" {
				if member.Name != "" {
					fnNames[member.Name] = true
				}
				continue
			}
			if member.Name != "" {
				fnNames[member.Name] = true
			}
			summary.TotalFunctions++
			hasHole := analyzeFunction(src, contract.Name, member, &summary, rep, standards, &warnedDynamicERC20)
			if hasHole {
				summary.HoleFunctions++
			} else {
				summary.CleanFunctions++
			}
		}
	}

	// Detectar estándares por firma de métodos (ERC-20 / ERC-721)
	if fnNames["transfer"] && fnNames["approve"] && fnNames["transferFrom"] && fnNames["balanceOf"] {
		standards["ERC-20 (candidato a GRC-20)"] = struct{}{}
	}
	if fnNames["ownerOf"] && fnNames["safeTransferFrom"] && fnNames["tokenURI"] {
		standards["ERC-721 (candidato a GRC-721)"] = struct{}{}
	}

	if summary.TotalFunctions > 0 {
		summary.DeterministicPct = (float64(summary.CleanFunctions) / float64(summary.TotalFunctions)) * 100.0
		// Si existen holes a nivel de contrato (p.ej. variable de estado int256), nunca reportar 100%
		if summary.HoleCount > 0 && summary.DeterministicPct == 100.0 {
			summary.DeterministicPct = 0.0
		}
	} else if summary.HoleCount == 0 {
		summary.DeterministicPct = 100.0
	}

	switch {
	case summary.HoleCount == 0:
		summary.RecommendedMode = "deterministic (--ai=off)"
	case summary.DeterministicPct >= 50:
		summary.RecommendedMode = "hybrid (--ai=assist para huecos puntuales)"
	default:
		summary.RecommendedMode = "ai-assisted (--ai=assist / reescritura idiomática)"
	}

	return summary
}

func analyzeVariableDecl(src *solc.SourceOutput, contractName string, v *solc.Node, summary *ContractSummary, rep *Report) {
	if wideType, ok := detectWideSignedType(v.TypeName, v.TypeDescriptions); ok {
		summary.HoleCount++
		rep.addFinding(Finding{
			Level:      LevelHole,
			Code:       "UNSUPPORTED_WIDE_SIGNED_INT",
			Contract:   contractName,
			Message:    fmt.Sprintf("Entero con signo >64 bits (%s) en variable de estado '%s' (no existe I256 en solrt todavía)", wideType, v.Name),
			Location:   solc.SrcLocation(src.Path, src.Content, v.Src),
			Suggestion: "Usa int64 o menor si cabe en 64 bits, o resuelve con --ai=assist / implementación I256 para evitar recorte silencioso.",
		})
	}
	initNode := v.InitialValue
	if initNode == nil {
		initNode = v.Value.Node
	}
	if initNode == nil {
		return
	}
	solc.Walk(initNode, func(n *solc.Node) bool {
		if isHardcodedEVMAddress(n) {
			summary.WarnCount++
			rep.addFinding(Finding{
				Level:      LevelWarn,
				Code:       "HARDCODED_EVM_ADDRESS",
				Contract:   contractName,
				Message:    fmt.Sprintf("Dirección EVM literal (%s) en variable de estado '%s'", n.Value.String, v.Name),
				Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
				Suggestion: "Sustituye la dirección 0x... por una dirección Bech32 de gno.land (g1...) o pásala por configuración/Init().",
			})
		}
		return true
	})
}

func analyzeFunction(
	src *solc.SourceOutput,
	contractName string,
	fn *solc.Node,
	summary *ContractSummary,
	rep *Report,
	standards map[string]struct{},
	warnedDynamicERC20 *bool,
) (hasHole bool) {
	fnLabel := fn.Name
	if fnLabel == "" {
		fnLabel = fn.Kind
	}

	// fallback() / receive()
	if fn.Kind == "fallback" || fn.Kind == "receive" {
		hasHole = true
		summary.HoleCount++
		rep.addFinding(Finding{
			Level:      LevelHole,
			Code:       "UNSUPPORTED_FALLBACK",
			Contract:   contractName,
			Function:   fnLabel,
			Message:    fmt.Sprintf("Función '%s()' no tiene equivalente directo en Gno", fn.Kind),
			Location:   solc.SrcLocation(src.Path, src.Content, fn.Src),
			Suggestion: "En Gno las llamadas se despachan por nombre de función explícito; expón un método con nombre.",
		})
	}

	// constructor con argumentos
	if fn.Kind == "constructor" && len(fn.Parameters.ParamNodes()) > 0 {
		summary.WarnCount++
		rep.addFinding(Finding{
			Level:      LevelWarn,
			Code:       "CONSTRUCTOR_WITH_ARGS",
			Contract:   contractName,
			Function:   "constructor",
			Message:    fmt.Sprintf("Constructor con %d parámetros (Gno 'func init()' no acepta argumentos)", len(fn.Parameters.ParamNodes())),
			Location:   solc.SrcLocation(src.Path, src.Content, fn.Src),
			Suggestion: "gnofy generará una función explícita Init(...) protegida por guard 'initialized' o configuración de despliegue.",
		})
	}

	// payable
	if fn.StateMutability == "payable" {
		summary.WarnCount++
		rep.addFinding(Finding{
			Level:      LevelWarn,
			Code:       "PAYABLE_NATIVE_VALUE",
			Contract:   contractName,
			Function:   fnLabel,
			Message:    fmt.Sprintf("Función '%s' es payable (recibe moneda nativa vía msg.value)", fnLabel),
			Location:   solc.SrcLocation(src.Path, src.Content, fn.Src),
			Suggestion: "En Gno los fondos adjuntos se leen con chain/banker (BankerTypeOriginSend) y denominación explícita (ej. ugnot).",
		})
	}

	// Comprobar parámetros y retornos por enteros con signo >64 bits (int72..int256)
	for _, p := range append(fn.Parameters.ParamNodes(), fn.ReturnParameters.ParamNodes()...) {
		if wideType, ok := detectWideSignedType(p.TypeName, p.TypeDescriptions); ok {
			hasHole = true
			summary.HoleCount++
			rep.addFinding(Finding{
				Level:      LevelHole,
				Code:       "UNSUPPORTED_WIDE_SIGNED_INT",
				Contract:   contractName,
				Function:   fnLabel,
				Message:    fmt.Sprintf("Entero con signo >64 bits (%s) en firma de '%s'", wideType, fnLabel),
				Location:   solc.SrcLocation(src.Path, src.Content, p.Src),
				Suggestion: "gnofy no recorta int256 a int64 en silencio; usa int64 o resuelve explícitamente.",
			})
		}
	}

	if fn.Body == nil {
		return hasHole
	}

	solc.Walk(fn.Body, func(n *solc.Node) bool {
		switch n.NodeType {
		case "VariableDeclaration":
			if wideType, ok := detectWideSignedType(n.TypeName, n.TypeDescriptions); ok {
				hasHole = true
				summary.HoleCount++
				rep.addFinding(Finding{
					Level:      LevelHole,
					Code:       "UNSUPPORTED_WIDE_SIGNED_INT",
					Contract:   contractName,
					Function:   fnLabel,
					Message:    fmt.Sprintf("Variable local con signo >64 bits (%s %s) en '%s'", wideType, n.Name, fnLabel),
					Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
					Suggestion: "gnofy no recorta int256 a int64 en silencio; usa int64 o resuelve explícitamente.",
				})
			}
		case "InlineAssembly":
			hasHole = true
			summary.HoleCount++
			rep.addFinding(Finding{
				Level:      LevelHole,
				Code:       "UNSUPPORTED_YUL",
				Contract:   contractName,
				Function:   fnLabel,
				Message:    fmt.Sprintf("Bloque inline assembly (Yul) en '%s'", fnLabel),
				Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
				Suggestion: "Usa --ai=assist (HoleResolver) o reescribe la lógica de alto nivel con primitives de Gno (banker/grc20).",
			})
			return false // no descender al sub-AST Yul

		case "TryStatement":
			hasHole = true
			summary.HoleCount++
			rep.addFinding(Finding{
				Level:      LevelHole,
				Code:       "UNSUPPORTED_TRY_CATCH",
				Contract:   contractName,
				Function:   fnLabel,
				Message:    fmt.Sprintf("Sentencia try/catch en '%s' (reversión parcial de estado distinta en GnoVM)", fnLabel),
				Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
				Suggestion: "Refactoriza devolviendo error explícito en Go/Gno en vez de capturar reverts de sub-llamadas.",
			})

		case "UncheckedBlock":
			summary.WarnCount++
			rep.addFinding(Finding{
				Level:      LevelWarn,
				Code:       "UNCHECKED_BLOCK",
				Contract:   contractName,
				Function:   fnLabel,
				Message:    fmt.Sprintf("Bloque unchecked {} en '%s'", fnLabel),
				Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
				Suggestion: "Se mapeará a operaciones Unchecked* en solrt (wrap-around explícito).",
			})

		case "NewExpression":
			if n.TypeName != nil && n.TypeName.NodeType == "UserDefinedTypeName" {
				hasHole = true
				summary.HoleCount++
				rep.addFinding(Finding{
					Level:      LevelHole,
					Code:       "UNSUPPORTED_CREATE",
					Contract:   contractName,
					Function:   fnLabel,
					Message:    fmt.Sprintf("Despliegue dinámico 'new %s(...)' en '%s'", n.TypeName.Name, fnLabel),
					Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
					Suggestion: "En Gno los Realms no despliegan otros Realms en runtime; usa structs/instancias gestionadas dentro de un Realm fábrica.",
				})
			}

		case "MemberAccess":
			switch n.MemberName {
			case "delegatecall":
				hasHole = true
				summary.HoleCount++
				rep.addFinding(Finding{
					Level:      LevelHole,
					Code:       "UNSUPPORTED_DELEGATECALL",
					Contract:   contractName,
					Function:   fnLabel,
					Message:    fmt.Sprintf("Llamada de bajo nivel '.delegatecall()' en '%s'", fnLabel),
					Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
					Suggestion: "Gno no soporta delegatecall/proxies EVM; sustituye por composición de packages o gobernanza nativa.",
				})
			case "call", "staticcall", "callcode":
				if isAddressType(n.Expression) {
					hasHole = true
					summary.HoleCount++
					rep.addFinding(Finding{
						Level:      LevelHole,
						Code:       "UNSUPPORTED_LOW_LEVEL_CALL",
						Contract:   contractName,
						Function:   fnLabel,
						Message:    fmt.Sprintf("Llamada de bajo nivel '.%s()' sobre address en '%s'", n.MemberName, fnLabel),
						Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
						Suggestion: "Usa llamadas tipadas a interfaces/packages Gno o chain/banker para transferencias nativas.",
					})
				}
			case "origin":
				if n.Expression != nil && n.Expression.NodeType == "Identifier" && n.Expression.Name == "tx" {
					summary.WarnCount++
					rep.addFinding(Finding{
						Level:      LevelWarn,
						Code:       "TX_ORIGIN_USAGE",
						Contract:   contractName,
						Function:   fnLabel,
						Message:    fmt.Sprintf("Uso de tx.origin en '%s'", fnLabel),
						Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
						Suggestion: "Se mapea a runtime.OriginCaller(); revisa si realmente necesitas origin o PreviousRealm().",
					})
				}
			case "balance":
				if isAddressType(n.Expression) {
					summary.WarnCount++
					rep.addFinding(Finding{
						Level:      LevelWarn,
						Code:       "NATIVE_BALANCE_QUERY",
						Contract:   contractName,
						Function:   fnLabel,
						Message:    fmt.Sprintf("Consulta de balance nativo '.balance' en '%s'", fnLabel),
						Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
						Suggestion: "Se mapea a banker.NewBanker(BankerTypeReadonly).GetCoins(addr).AmountOf(denom).",
					})
				}
			case "mulDiv":
				if n.Expression != nil && n.Expression.Name == "PRBMath" {
					standards["PRBMath (mulDiv → solrt.MulDiv)"] = struct{}{}
				}
			}

			// this.foo()
			if n.Expression != nil && n.Expression.NodeType == "Identifier" && n.Expression.Name == "this" {
				summary.WarnCount++
				rep.addFinding(Finding{
					Level:      LevelWarn,
					Code:       "EXTERNAL_SELF_CALL",
					Contract:   contractName,
					Function:   fnLabel,
					Message:    fmt.Sprintf("Auto-llamada externa 'this.%s(...)' en '%s'", n.MemberName, fnLabel),
					Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
					Suggestion: "En Gno se simplifica a llamada local directa dentro del mismo Realm.",
				})
			}

			// SafeERC20.*
			if n.Expression != nil && n.Expression.NodeType == "Identifier" && n.Expression.Name == "SafeERC20" {
				standards["ERC-20 / SafeERC20"] = struct{}{}
			}

		case "FunctionCall":
			// IERC20(address)
			if n.Kind == "typeConversion" && n.Expression != nil && n.Expression.Name == "IERC20" {
				standards["ERC-20 / SafeERC20"] = struct{}{}
				if !*warnedDynamicERC20 {
					*warnedDynamicERC20 = true
					summary.WarnCount++
					rep.addFinding(Finding{
						Level:      LevelWarn,
						Code:       "DYNAMIC_ERC20_CAST",
						Contract:   contractName,
						Function:   fnLabel,
						Message:    "Cast dinámico IERC20(address) detectado (despacho de tokens por dirección en runtime)",
						Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
						Suggestion: "En Gno no se castea una address a interfaz en runtime; usa grc20.Teller o un registro de tokens (grc20reg).",
					})
				}
			}

		case "Identifier":
			if n.Name == "selfdestruct" {
				hasHole = true
				summary.HoleCount++
				rep.addFinding(Finding{
					Level:      LevelHole,
					Code:       "UNSUPPORTED_SELFDESTRUCT",
					Contract:   contractName,
					Function:   fnLabel,
					Message:    fmt.Sprintf("Uso de selfdestruct en '%s'", fnLabel),
					Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
					Suggestion: "Sustituye por un flag 'paused/deprecated' y retiro de fondos vía banker.",
				})
			}

		case "Literal":
			if isHardcodedEVMAddress(n) {
				summary.WarnCount++
				rep.addFinding(Finding{
					Level:      LevelWarn,
					Code:       "HARDCODED_EVM_ADDRESS",
					Contract:   contractName,
					Function:   fnLabel,
					Message:    fmt.Sprintf("Dirección EVM literal (%s) en '%s'", n.Value.String, fnLabel),
					Location:   solc.SrcLocation(src.Path, src.Content, n.Src),
					Suggestion: "Sustituye por una dirección Bech32 (g1...) de gno.land.",
				})
			}
		}
		return true
	})

	return hasHole
}

func isHardcodedEVMAddress(n *solc.Node) bool {
	if n == nil || n.NodeType != "Literal" || n.TypeDescriptions == nil {
		return false
	}
	if !strings.HasPrefix(n.TypeDescriptions.TypeIdentifier, "t_address") {
		return false
	}
	val := strings.ToLower(strings.TrimSpace(n.Value.String))
	if !strings.HasPrefix(val, "0x") || len(val) != 42 {
		return false
	}
	// Ignorar address(0) / 0x000...000
	return strings.Trim(val[2:], "0") != ""
}

func isAddressType(n *solc.Node) bool {
	if n == nil || n.TypeDescriptions == nil {
		return false
	}
	ti := n.TypeDescriptions.TypeIdentifier
	return strings.HasPrefix(ti, "t_address") || strings.Contains(n.TypeDescriptions.TypeString, "address")
}

func detectWideSignedType(tn *solc.Node, td *solc.TypeDescriptions) (string, bool) {
	check := func(s string) (string, bool) {
		for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
		}) {
			if tok == "int" {
				return "int256", true
			}
			if strings.HasPrefix(tok, "int") && len(tok) > 3 {
				bits := 0
				valid := true
				for i := 3; i < len(tok); i++ {
					if tok[i] < '0' || tok[i] > '9' {
						valid = false
						break
					}
					bits = bits*10 + int(tok[i]-'0')
				}
				if valid && bits > 64 {
					return tok, true
				}
			}
		}
		return "", false
	}
	if tn != nil && tn.Name != "" {
		if w, ok := check(tn.Name); ok {
			return w, true
		}
	}
	if td != nil && td.TypeString != "" {
		if w, ok := check(td.TypeString); ok {
			return w, true
		}
	}
	return "", false
}

func detectImportStandard(imp string, standards map[string]struct{}) {
	lower := strings.ToLower(imp)
	switch {
	case strings.Contains(lower, "reentrancyguard"):
		standards["OpenZeppelin ReentrancyGuard"] = struct{}{}
	case strings.Contains(lower, "safeerc20") || strings.Contains(lower, "ierc20"):
		standards["ERC-20 / SafeERC20"] = struct{}{}
	case strings.Contains(lower, "ownable"):
		standards["OpenZeppelin Ownable (candidato a p/demo/ownable)"] = struct{}{}
	case strings.Contains(lower, "pausable"):
		standards["OpenZeppelin Pausable"] = struct{}{}
	case strings.Contains(lower, "prbmath"):
		standards["PRBMath (mulDiv → solrt.MulDiv)"] = struct{}{}
	}
}

func detectBaseStandard(name string, standards map[string]struct{}) {
	switch name {
	case "ReentrancyGuard":
		standards["OpenZeppelin ReentrancyGuard"] = struct{}{}
	case "Ownable":
		standards["OpenZeppelin Ownable (candidato a p/demo/ownable)"] = struct{}{}
	case "Pausable":
		standards["OpenZeppelin Pausable"] = struct{}{}
	case "ERC20":
		standards["ERC-20 (candidato a GRC-20)"] = struct{}{}
	case "ERC721":
		standards["ERC-721 (candidato a GRC-721)"] = struct{}{}
	}
}
