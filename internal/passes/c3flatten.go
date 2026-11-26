package passes

import (
	"fmt"
	"slices"

	"github.com/gnofy-xyz/gnofy/internal/ir"
)

// C3FlattenPass aplica linealización C3 (idéntica a Solidity) para aplanar herencia
// múltiple (virtual / override / super) en contratos Gno sin herencia de clases.
type C3FlattenPass struct{}

func (C3FlattenPass) Name() string { return "c3flatten" }

func (C3FlattenPass) Run(mod *ir.Module) error {
	byName := make(map[string]*ir.Contract, len(mod.Contracts))
	for _, c := range mod.Contracts {
		byName[c.Name] = c
	}

	memo := make(map[string][]*ir.Contract)
	for _, c := range mod.Contracts {
		if c.Kind != ir.KindContract || len(c.BaseNames) == 0 {
			continue
		}
		lin, err := linearizeC3(c, byName, memo)
		if err != nil {
			return err
		}
		if len(lin) <= 1 {
			continue
		}
		flattenInto(c, lin)
	}
	return nil
}

// linearizeC3 calcula la cadena C3 de más derivado a más base (en Solidity, los bases
// a la derecha tienen mayor precedencia: `contract C is A, B` => B antes que A).
func linearizeC3(c *ir.Contract, byName map[string]*ir.Contract, memo map[string][]*ir.Contract) ([]*ir.Contract, error) {
	if cached, ok := memo[c.Name]; ok {
		return cached, nil
	}

	var parents []*ir.Contract
	for i := len(c.BaseNames) - 1; i >= 0; i-- {
		if base, ok := byName[c.BaseNames[i]]; ok {
			parents = append(parents, base)
		}
	}
	if len(parents) == 0 {
		res := []*ir.Contract{c}
		memo[c.Name] = res
		return res, nil
	}

	var seqs [][]*ir.Contract
	for _, p := range parents {
		pl, err := linearizeC3(p, byName, memo)
		if err != nil {
			return nil, err
		}
		seqs = append(seqs, slices.Clone(pl))
	}
	seqs = append(seqs, slices.Clone(parents))

	merged, err := c3Merge(seqs)
	if err != nil {
		return nil, fmt.Errorf("C3 linearization falló para %s: %w", c.Name, err)
	}
	res := append([]*ir.Contract{c}, merged...)
	memo[c.Name] = res
	return res, nil
}

func c3Merge(seqs [][]*ir.Contract) ([]*ir.Contract, error) {
	var out []*ir.Contract
	for {
		// Filtrar secuencias vacías
		var nonEmpty [][]*ir.Contract
		for _, s := range seqs {
			if len(s) > 0 {
				nonEmpty = append(nonEmpty, s)
			}
		}
		if len(nonEmpty) == 0 {
			return out, nil
		}
		seqs = nonEmpty

		var candidate *ir.Contract
		for _, s := range seqs {
			head := s[0]
			inTail := false
			for _, other := range seqs {
				for _, item := range other[1:] {
					if item.Name == head.Name {
						inTail = true
						break
					}
				}
				if inTail {
					break
				}
			}
			if !inTail {
				candidate = head
				break
			}
		}
		if candidate == nil {
			return nil, fmt.Errorf("jerarquía de herencia inconsistente (ciclo C3)")
		}

		out = append(out, candidate)
		for i := range seqs {
			if len(seqs[i]) > 0 && seqs[i][0].Name == candidate.Name {
				seqs[i] = seqs[i][1:]
			}
		}
	}
}

type fnImpl struct {
	owner string
	fn    *ir.Function
}

func flattenInto(derived *ir.Contract, lin []*ir.Contract) {
	// lin[0] es derived, lin[1:] son los ancestros ordenados de más derivado a más base.
	seenEnums := make(map[string]bool)
	seenStructs := make(map[string]bool)
	seenEvents := make(map[string]bool)
	seenErrors := make(map[string]bool)
	seenVars := make(map[string]bool)
	seenMods := make(map[string]bool)

	for _, e := range derived.Enums {
		seenEnums[e.Name] = true
	}
	for _, s := range derived.Structs {
		seenStructs[s.Name] = true
	}
	for _, ev := range derived.Events {
		seenEvents[ev.Name] = true
	}
	for _, er := range derived.Errors {
		seenErrors[er.Name] = true
	}
	for _, v := range derived.StateVars {
		seenVars[v.Name] = true
	}
	for _, m := range derived.Modifiers {
		seenMods[m.Name] = true
	}

	// Mapa de implementaciones por función a lo largo de la cadena C3 (más derivado primero)
	fnChain := make(map[string][]fnImpl)
	var fnOrder []string

	for _, ancestor := range lin {
		if ancestor != derived {
			if ancestor.UsesReentrancyGuard {
				derived.UsesReentrancyGuard = true
			}
			for _, e := range ancestor.Enums {
				if !seenEnums[e.Name] {
					seenEnums[e.Name] = true
					derived.Enums = append(derived.Enums, e)
				}
			}
			for _, s := range ancestor.Structs {
				if !seenStructs[s.Name] {
					seenStructs[s.Name] = true
					derived.Structs = append(derived.Structs, s)
				}
			}
			for _, ev := range ancestor.Events {
				if !seenEvents[ev.Name] {
					seenEvents[ev.Name] = true
					derived.Events = append(derived.Events, ev)
				}
			}
			for _, er := range ancestor.Errors {
				if !seenErrors[er.Name] {
					seenErrors[er.Name] = true
					derived.Errors = append(derived.Errors, er)
				}
			}
			for _, v := range ancestor.StateVars {
				if !seenVars[v.Name] {
					seenVars[v.Name] = true
					derived.StateVars = append(derived.StateVars, v)
				}
			}
			for _, m := range ancestor.Modifiers {
				if !seenMods[m.Name] {
					seenMods[m.Name] = true
					derived.Modifiers = append(derived.Modifiers, m)
				}
			}
		}

		for _, fn := range ancestor.Functions {
			if fn.IsInit {
				continue // constructores de base se gestionan aparte si aplica
			}
			if _, ok := fnChain[fn.Name]; !ok {
				fnOrder = append(fnOrder, fn.Name)
			}
			fnChain[fn.Name] = append(fnChain[fn.Name], fnImpl{owner: ancestor.Name, fn: fn})
		}
	}

	// Recolectar constructores y argumentos base en orden C3 (de más base a más derivado)
	baseCtors := make(map[string]*ir.Function)
	baseCtorArgs := make(map[string][]ir.Expr)
	ancestorNames := make(map[string]bool, len(lin))
	for _, anc := range lin[1:] {
		ancestorNames[anc.Name] = true
		for _, fn := range anc.Functions {
			if fn.IsInit {
				baseCtors[anc.Name] = fn
			}
		}
	}
	// Recolectar argumentos pasados en especificadores de herencia (de más derivado a más base)
	for _, anc := range lin {
		for baseName, args := range anc.BaseCtorArgs {
			if _, exists := baseCtorArgs[baseName]; !exists {
				baseCtorArgs[baseName] = args
			}
		}
	}

	// Construir lista final de funciones: constructor encadenado (si hay alguno) + métodos derivados + helpers super_*
	var derivedCtor *ir.Function
	for _, fn := range derived.Functions {
		if fn.IsInit {
			derivedCtor = cloneFunc(fn)
			break
		}
	}

	// Extraer invocaciones a constructores base desde los modifiers del constructor derivado
	// (ej. `constructor(uint256 c) Ownable(msg.sender) { ... }`)
	if derivedCtor != nil && len(derivedCtor.Modifiers) > 0 {
		var remainingMods []ir.ModifierCall
		for _, mc := range derivedCtor.Modifiers {
			if ancestorNames[mc.Name] {
				if _, exists := baseCtorArgs[mc.Name]; !exists {
					baseCtorArgs[mc.Name] = mc.Args
				}
				continue
			}
			remainingMods = append(remainingMods, mc)
		}
		derivedCtor.Modifiers = remainingMods
	}

	// Encadenar constructores base en orden inverso C3: más base primero -> más derivado último
	var preStmts []ir.Stmt
	for i := len(lin) - 1; i >= 1; i-- {
		base := lin[i]
		bCtor, ok := baseCtors[base.Name]
		if !ok {
			continue
		}
		if len(bCtor.Params) == 0 {
			preStmts = append(preStmts, &ir.BlockStmt{Stmts: slices.Clone(bCtor.Body)})
			continue
		}
		args, hasArgs := baseCtorArgs[base.Name]
		if hasArgs && len(args) == len(bCtor.Params) {
			var block []ir.Stmt
			for pi, p := range bCtor.Params {
				block = append(block, &ir.VarDeclStmt{
					Vars: []ir.Param{p},
					Init: args[pi],
				})
			}
			block = append(block, slices.Clone(bCtor.Body)...)
			preStmts = append(preStmts, &ir.BlockStmt{Stmts: block})
			continue
		}
		// Nunca omitir silenciosamente un constructor base parametrizado sin argumentos resueltos
		h := &ir.Hole{
			ID:               "ctor_" + base.Name,
			Reason:           "UNRESOLVED_BASE_CONSTRUCTOR",
			OriginalSolidity: fmt.Sprintf("base constructor %s(...) requires %d arg(s)", base.Name, len(bCtor.Params)),
			ScopeSummary:     "constructor",
		}
		derived.Holes = append(derived.Holes, h)
		preStmts = append(preStmts, &ir.HoleStmt{Hole: h})
	}

	var finalFns []*ir.Function
	if len(preStmts) > 0 {
		if derivedCtor == nil {
			derivedCtor = &ir.Function{
				Kind:       "constructor",
				IsInit:     true,
				Visibility: ir.VisPublic,
			}
		}
		derivedCtor.Body = append(preStmts, derivedCtor.Body...)
	}
	if derivedCtor != nil {
		finalFns = append(finalFns, derivedCtor)
	}

	superHelpers := make(map[string]*ir.Function)

	for _, name := range fnOrder {
		chain := fnChain[name]
		if len(chain) == 0 {
			continue
		}
		top := cloneFunc(chain[0].fn)
		rewriteSuperCalls(top.Body, chain, 0, superHelpers)
		finalFns = append(finalFns, top)
	}

	// Añadir helpers super_* necesarios en orden determinista
	var helperNames []string
	for k := range superHelpers {
		helperNames = append(helperNames, k)
	}
	slices.Sort(helperNames)
	for _, k := range helperNames {
		finalFns = append(finalFns, superHelpers[k])
	}

	derived.Functions = finalFns
}

func cloneFunc(fn *ir.Function) *ir.Function {
	cp := *fn
	cp.Params = slices.Clone(fn.Params)
	cp.Returns = slices.Clone(fn.Returns)
	cp.Modifiers = slices.Clone(fn.Modifiers)
	cp.Body = slices.Clone(fn.Body)
	return &cp
}

func rewriteSuperCalls(stmts []ir.Stmt, chain []fnImpl, idx int, helpers map[string]*ir.Function) {
	for i := range stmts {
		stmts[i] = rewriteStmtSuper(stmts[i], chain, idx, helpers)
	}
}

func rewriteStmtSuper(s ir.Stmt, chain []fnImpl, idx int, helpers map[string]*ir.Function) ir.Stmt {
	switch st := s.(type) {
	case *ir.BlockStmt:
		cp := &ir.BlockStmt{Stmts: slices.Clone(st.Stmts)}
		rewriteSuperCalls(cp.Stmts, chain, idx, helpers)
		return cp
	case *ir.ReturnStmt:
		return &ir.ReturnStmt{Value: rewriteExprSuper(st.Value, chain, idx, helpers)}
	case *ir.ExprStmt:
		return &ir.ExprStmt{Expr: rewriteExprSuper(st.Expr, chain, idx, helpers)}
	case *ir.AssignStmt:
		return &ir.AssignStmt{
			Op:  st.Op,
			LHS: rewriteExprSuper(st.LHS, chain, idx, helpers),
			RHS: rewriteExprSuper(st.RHS, chain, idx, helpers),
		}
	case *ir.VarDeclStmt:
		cp := *st
		cp.Init = rewriteExprSuper(st.Init, chain, idx, helpers)
		return &cp
	case *ir.IfStmt:
		cp := *st
		cp.Cond = rewriteExprSuper(st.Cond, chain, idx, helpers)
		cp.Then = slices.Clone(st.Then)
		cp.Else = slices.Clone(st.Else)
		rewriteSuperCalls(cp.Then, chain, idx, helpers)
		rewriteSuperCalls(cp.Else, chain, idx, helpers)
		return &cp
	default:
		return s
	}
}

func rewriteExprSuper(e ir.Expr, chain []fnImpl, idx int, helpers map[string]*ir.Function) ir.Expr {
	if e == nil {
		return nil
	}
	switch ex := e.(type) {
	case *ir.CallExpr:
		if mem, ok := ex.Callee.(*ir.MemberExpr); ok {
			if id, ok := mem.Object.(*ir.IdentExpr); ok && id.Name == "super" && idx+1 < len(chain) {
				next := chain[idx+1]
				helperName := "super_" + next.owner + "_" + mem.Member
				if _, exists := helpers[helperName]; !exists {
					hfn := cloneFunc(next.fn)
					hfn.Name = helperName
					hfn.Visibility = ir.VisPrivate
					hfn.Modifiers = nil
					helpers[helperName] = hfn
					rewriteSuperCalls(hfn.Body, chain, idx+1, helpers)
				}
				cp := *ex
				cp.Callee = &ir.IdentExpr{Name: helperName, Type: mem.Type}
				cp.Args = slices.Clone(ex.Args)
				for i := range cp.Args {
					cp.Args[i] = rewriteExprSuper(cp.Args[i], chain, idx, helpers)
				}
				return &cp
			}
		}
		cp := *ex
		cp.Callee = rewriteExprSuper(ex.Callee, chain, idx, helpers)
		cp.Args = slices.Clone(ex.Args)
		for i := range cp.Args {
			cp.Args[i] = rewriteExprSuper(cp.Args[i], chain, idx, helpers)
		}
		return &cp
	case *ir.BinaryExpr:
		cp := *ex
		cp.Left = rewriteExprSuper(ex.Left, chain, idx, helpers)
		cp.Right = rewriteExprSuper(ex.Right, chain, idx, helpers)
		return &cp
	default:
		return e
	}
}
