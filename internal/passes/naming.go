package passes

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/gnofy-xyz/gnofy/internal/ir"
)

// NamingPass adapta nombres Solidity a convenciones Go/Gno (visibilidad por mayúscula,
// getters de variables de estado públicas, desambiguación de sobrecargas y keywords).
type NamingPass struct{}

func (NamingPass) Name() string { return "naming" }

var goKeywords = map[string]bool{
	// Keywords Go / Gno
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	"realm": true, "cur": true,
	// Builtins del universo Gno que NO pueden ser sombreados (gnoPreprocessError)
	"append": true, "cap": true, "close": true, "complex": true, "copy": true,
	"delete": true, "imag": true, "len": true, "make": true, "new": true,
	"panic": true, "print": true, "println": true, "real": true, "recover": true,
	"revive": true, "cross": true, "address": true, "any": true, "bool": true,
	"byte": true, "error": true, "rune": true, "string": true, "true": true,
	"false": true, "nil": true, "iota": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "float32": true, "float64": true,
}

func (NamingPass) Run(mod *ir.Module) error {
	for _, c := range mod.Contracts {
		c.GoPkgName = sanitizePkgName(c.Name)

		for _, e := range c.Enums {
			e.GoName = exportIdent(e.Name)
		}
		for _, s := range c.Structs {
			s.GoName = exportIdent(s.Name)
			for i := range s.Fields {
				s.Fields[i].GoName = exportIdent(s.Fields[i].Name)
			}
		}

		// Variables de estado siempre en minúscula en paquete para no colisionar con getters exportados
		stateGoNames := make(map[string]bool, len(c.StateVars))
		for _, sv := range c.StateVars {
			sv.GoName = unexportIdent(sv.Name)
			stateGoNames[sv.GoName] = true
			if sv.Visibility == ir.VisPublic {
				sv.GetterGoName = exportIdent(sv.Name)
			}
		}

		usedFuncNames := make(map[string]int)
		for _, fn := range c.Functions {
			for i := range fn.Params {
				pName := fn.Params[i].Name
				if pName == "" {
					pName = fmt.Sprintf("arg%d", i)
				}
				san := sanitizeLocalIdent(pName)
				if stateGoNames[san] {
					san += "Param"
				}
				fn.Params[i].GoName = san
			}
			for i := range fn.Returns {
				if fn.Returns[i].Name != "" {
					fn.Returns[i].GoName = sanitizeLocalIdent(fn.Returns[i].Name)
				}
			}

			var base string
			switch {
			case fn.IsInit && len(fn.Params) == 0:
				base = "init"
			case fn.IsInit && len(fn.Params) > 0:
				base = "Init"
			case fn.Kind == "fallback":
				base = "Fallback"
			case fn.Kind == "receive":
				base = "Receive"
			case fn.Visibility == ir.VisPublic || fn.Visibility == ir.VisExternal:
				base = exportIdent(fn.Name)
			default:
				base = unexportIdent(fn.Name)
			}

			if base != "init" {
				usedFuncNames[base]++
				if usedFuncNames[base] > 1 {
					base = fmt.Sprintf("%s_%d", base, usedFuncNames[base])
				}
			}
			fn.GoName = base
		}

		// Sintetizar getters para variables de estado public sin colisionar con funciones existentes
		for _, sv := range c.StateVars {
			if sv.Visibility != ir.VisPublic {
				continue
			}
			if usedFuncNames[sv.GetterGoName] > 0 {
				continue
			}
			usedFuncNames[sv.GetterGoName]++
			getter := synthesizeGetter(sv)
			c.Functions = append(c.Functions, getter)
		}
	}
	return nil
}

func synthesizeGetter(sv *ir.StateVar) *ir.Function {
	fn := &ir.Function{
		Name:       sv.Name,
		GoName:     sv.GetterGoName,
		Kind:       "function",
		IsGetter:   true,
		ViewOrPure: true,
		Visibility: ir.VisPublic,
	}

	// Construir parámetros y expresión de acceso según niveles de mapping
	var expr ir.Expr = &ir.IdentExpr{Name: sv.Name, Type: sv.Type}
	curType := sv.Type
	idx := 0
	for curType.Kind == "mapping" && curType.KeyType != nil && curType.ValueType != nil {
		pName := fmt.Sprintf("k%d", idx)
		fn.Params = append(fn.Params, ir.Param{
			Name:   pName,
			GoName: pName,
			Type:   *curType.KeyType,
		})
		expr = &ir.IndexExpr{
			Base:  expr,
			Index: &ir.IdentExpr{Name: pName, Type: *curType.KeyType},
			Type:  *curType.ValueType,
		}
		curType = *curType.ValueType
		idx++
	}

	fn.Returns = []ir.Param{{Type: curType}}
	fn.Body = []ir.Stmt{&ir.ReturnStmt{Value: expr}}
	return fn
}

func sanitizePkgName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "contract"
	}
	s := b.String()
	if s[0] >= '0' && s[0] <= '9' {
		return "c" + s
	}
	return s
}

func exportIdent(name string) string {
	trimmed := strings.TrimLeft(name, "_")
	if trimmed == "" {
		return "Exported"
	}
	parts := strings.Split(trimmed, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	res := b.String()
	if goKeywords[res] {
		return res + "Val"
	}
	return res
}

func unexportIdent(name string) string {
	trimmed := strings.TrimLeft(name, "_")
	if trimmed == "" {
		return "val"
	}
	parts := strings.Split(trimmed, "_")
	var b strings.Builder
	first := true
	for _, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		if first {
			runes[0] = unicode.ToLower(runes[0])
			first = false
		} else {
			runes[0] = unicode.ToUpper(runes[0])
		}
		b.WriteString(string(runes))
	}
	res := b.String()
	if goKeywords[res] {
		return res + "Val"
	}
	return res
}

func sanitizeLocalIdent(name string) string {
	if name == "_" {
		return "_"
	}
	res := unexportIdent(name)
	if res == "" {
		return "arg"
	}
	if goKeywords[res] {
		return res + "Arg"
	}
	return res
}
