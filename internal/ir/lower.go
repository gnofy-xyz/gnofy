package ir

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
)

// Lower convierte el resultado compilado por solc en una Module IR determinista.
func Lower(res *solc.CompileResult) *Module {
	mod := &Module{Name: "gnofy"}

	for _, filePath := range res.TargetFiles {
		src, ok := res.Sources[filePath]
		if !ok || src.AST == nil || src.IsShim {
			continue
		}
		lowerSourceUnit(src, mod)
	}
	return mod
}

func lowerSourceUnit(src *solc.SourceOutput, mod *Module) {
	var topEvents []*EventDef
	var topErrors []*ErrorDef

	for _, n := range src.AST.Nodes {
		switch n.NodeType {
		case "EventDefinition":
			topEvents = append(topEvents, lowerEvent(n))
		case "ErrorDefinition":
			topErrors = append(topErrors, lowerError(n))
		case "ContractDefinition":
			c := lowerContract(src, n, topEvents, topErrors)
			mod.Contracts = append(mod.Contracts, c)
		}
	}
}

func lowerContract(src *solc.SourceOutput, n *solc.Node, topEvents []*EventDef, topErrors []*ErrorDef) *Contract {
	c := &Contract{
		Name:       n.Name,
		Kind:       ContractKind(n.ContractKind),
		Abstract:   n.Abstract,
		SourcePath: src.Path,
	}
	c.Events = append(c.Events, topEvents...)
	c.Errors = append(c.Errors, topErrors...)

	for _, b := range n.BaseContracts {
		if b.BaseName != nil && b.BaseName.Name != "" {
			c.BaseNames = append(c.BaseNames, b.BaseName.Name)
			if b.BaseName.Name == "ReentrancyGuard" {
				c.UsesReentrancyGuard = true
			}
		}
	}

	for _, member := range n.Nodes {
		switch member.NodeType {
		case "EnumDefinition":
			e := &EnumDef{Name: member.Name}
			for _, m := range member.Members {
				e.Members = append(e.Members, m.Name)
			}
			c.Enums = append(c.Enums, e)

		case "StructDefinition":
			s := &StructDef{Name: member.Name}
			for _, f := range member.Members {
				s.Fields = append(s.Fields, Param{
					Name: f.Name,
					Type: lowerTypeName(f.TypeName, f.TypeDescriptions),
				})
			}
			c.Structs = append(c.Structs, s)

		case "EventDefinition":
			c.Events = append(c.Events, lowerEvent(member))

		case "ErrorDefinition":
			c.Errors = append(c.Errors, lowerError(member))

		case "VariableDeclaration":
			sv := &StateVar{
				Name:       member.Name,
				Type:       lowerTypeName(member.TypeName, member.TypeDescriptions),
				Visibility: Visibility(member.Visibility),
				Constant:   member.Constant,
				Immutable:  member.Immutable,
			}
			initNode := member.InitialValue
			if initNode == nil {
				initNode = member.Value.Node
			}
			if initNode != nil {
				sv.InitialValue = lowerExpr(src, c, nil, initNode)
			}
			c.StateVars = append(c.StateVars, sv)

		case "ModifierDefinition":
			modDef := &ModifierDef{
				Name:   member.Name,
				Params: lowerParams(member.Parameters),
			}
			if member.Body != nil {
				modDef.Body = lowerBlockStmts(src, c, nil, member.Body)
			}
			c.Modifiers = append(c.Modifiers, modDef)

		case "FunctionDefinition":
			fn := lowerFunction(src, c, member)
			c.Functions = append(c.Functions, fn)
		}
	}

	return c
}

func lowerEvent(n *solc.Node) *EventDef {
	return &EventDef{
		Name:   n.Name,
		Params: lowerParams(n.Parameters),
	}
}

func lowerError(n *solc.Node) *ErrorDef {
	return &ErrorDef{
		Name:   n.Name,
		Params: lowerParams(n.Parameters),
	}
}

func lowerFunction(src *solc.SourceOutput, c *Contract, n *solc.Node) *Function {
	fn := &Function{
		Name:       n.Name,
		Kind:       n.Kind,
		IsInit:     n.Kind == "constructor",
		Virtual:    n.Virtual,
		IsOverride: n.Overrides != nil,
		Payable:    n.StateMutability == "payable",
		ViewOrPure: n.StateMutability == "view" || n.StateMutability == "pure",
		Visibility: Visibility(n.Visibility),
		Params:     lowerParams(n.Parameters),
		Returns:    lowerParams(n.ReturnParameters),
	}

	for _, mod := range n.Modifiers {
		mc := ModifierCall{}
		if mod.ModifierName != nil {
			mc.Name = mod.ModifierName.Name
		}
		for _, a := range mod.Arguments {
			mc.Args = append(mc.Args, lowerExpr(src, c, fn, a))
		}
		fn.Modifiers = append(fn.Modifiers, mc)
	}

	if n.Kind == "fallback" || n.Kind == "receive" {
		h := newHole("UNSUPPORTED_FALLBACK", extractSnippet(src.Content, n.Src), fn.Kind)
		fn.Holes = append(fn.Holes, h)
		c.Holes = append(c.Holes, h)
		fn.Body = []Stmt{&HoleStmt{Hole: h}}
		return fn
	}

	if n.Body != nil {
		fn.Body = lowerBlockStmts(src, c, fn, n.Body)
	}
	return fn
}

func lowerBlockStmts(src *solc.SourceOutput, c *Contract, fn *Function, block *solc.Node) []Stmt {
	if block == nil {
		return nil
	}
	if block.NodeType != "Block" && block.NodeType != "UncheckedBlock" {
		if s := lowerStmt(src, c, fn, block); s != nil {
			return []Stmt{s}
		}
		return nil
	}
	var out []Stmt
	for _, s := range block.Statements {
		if ls := lowerStmt(src, c, fn, s); ls != nil {
			out = append(out, ls)
		}
	}
	return out
}

func lowerStmt(src *solc.SourceOutput, c *Contract, fn *Function, n *solc.Node) Stmt {
	if n == nil {
		return nil
	}
	switch n.NodeType {
	case "Block":
		return &BlockStmt{Stmts: lowerBlockStmts(src, c, fn, n)}

	case "UncheckedBlock":
		return &UncheckedStmt{Body: lowerBlockStmts(src, c, fn, n)}

	case "PlaceholderStatement":
		return &PlaceholderStmt{}

	case "Break":
		return &BreakStmt{}

	case "Continue":
		return &ContinueStmt{}

	case "InlineAssembly":
		h := newHole("UNSUPPORTED_YUL", extractSnippet(src.Content, n.Src), fnScopeName(fn))
		recordHole(c, fn, h)
		return &HoleStmt{Hole: h}

	case "TryStatement":
		h := newHole("UNSUPPORTED_TRY_CATCH", extractSnippet(src.Content, n.Src), fnScopeName(fn))
		recordHole(c, fn, h)
		return &HoleStmt{Hole: h}

	case "VariableDeclarationStatement":
		stmt := &VarDeclStmt{}
		for _, d := range n.Declarations {
			if d == nil {
				stmt.Vars = append(stmt.Vars, Param{Name: "_"})
				continue
			}
			if d.StorageLocation == "storage" {
				stmt.IsStorage = true
			}
			stmt.Vars = append(stmt.Vars, Param{
				Name: d.Name,
				Type: lowerTypeName(d.TypeName, d.TypeDescriptions),
			})
		}
		if n.InitialValue != nil {
			stmt.Init = lowerExpr(src, c, fn, n.InitialValue)
		}
		return stmt

	case "ExpressionStatement":
		if n.Expression != nil && n.Expression.NodeType == "Assignment" {
			return &AssignStmt{
				Op:  n.Expression.Operator,
				LHS: lowerExpr(src, c, fn, n.Expression.LeftHandSide),
				RHS: lowerExpr(src, c, fn, n.Expression.RightHandSide),
			}
		}
		return &ExprStmt{Expr: lowerExpr(src, c, fn, n.Expression)}

	case "IfStatement":
		return &IfStmt{
			Cond: lowerExpr(src, c, fn, n.Condition),
			Then: lowerBlockStmts(src, c, fn, n.TrueBody),
			Else: lowerBlockStmts(src, c, fn, n.FalseBody),
		}

	case "ForStatement":
		return &ForStmt{
			Init: lowerStmt(src, c, fn, n.InitializationExpression),
			Cond: lowerExpr(src, c, fn, n.Condition),
			Post: lowerStmt(src, c, fn, n.LoopExpression),
			Body: lowerBlockStmts(src, c, fn, n.Body),
		}

	case "WhileStatement":
		return &WhileStmt{
			Cond: lowerExpr(src, c, fn, n.Condition),
			Body: lowerBlockStmts(src, c, fn, n.Body),
		}

	case "Return":
		var val Expr
		if n.Expression != nil {
			val = lowerExpr(src, c, fn, n.Expression)
		}
		return &ReturnStmt{Value: val}

	case "EmitStatement":
		es := &EmitStmt{}
		if n.EventCall != nil {
			if n.EventCall.Expression != nil {
				es.EventName = n.EventCall.Expression.Name
			}
			for _, a := range n.EventCall.Arguments {
				es.Args = append(es.Args, lowerExpr(src, c, fn, a))
			}
		}
		return es

	case "RevertStatement":
		rs := &RevertStmt{}
		if n.ErrorCall != nil {
			if n.ErrorCall.Expression != nil {
				rs.ErrorName = n.ErrorCall.Expression.Name
			}
			for _, a := range n.ErrorCall.Arguments {
				rs.Args = append(rs.Args, lowerExpr(src, c, fn, a))
			}
		}
		return rs

	default:
		h := newHole("UNSUPPORTED_STMT_"+n.NodeType, extractSnippet(src.Content, n.Src), fnScopeName(fn))
		recordHole(c, fn, h)
		return &HoleStmt{Hole: h}
	}
}

func lowerExpr(src *solc.SourceOutput, c *Contract, fn *Function, n *solc.Node) Expr {
	if n == nil {
		return nil
	}
	typ := typeFromDescriptions(n.TypeDescriptions)

	switch n.NodeType {
	case "Literal":
		kind := n.Kind
		if typ.Kind == "address" {
			kind = "address"
		}
		return &LiteralExpr{Kind: kind, Value: n.Value.String, Type: typ}

	case "Identifier":
		if n.Name == "selfdestruct" {
			h := newHole("UNSUPPORTED_SELFDESTRUCT", extractSnippet(src.Content, n.Src), fnScopeName(fn))
			recordHole(c, fn, h)
			return &HoleExpr{Hole: h, Type: typ}
		}
		return &IdentExpr{Name: n.Name, Type: typ}

	case "MemberAccess":
		if n.MemberName == "delegatecall" {
			h := newHole("UNSUPPORTED_DELEGATECALL", extractSnippet(src.Content, n.Src), fnScopeName(fn))
			recordHole(c, fn, h)
			return &HoleExpr{Hole: h, Type: typ}
		}
		if (n.MemberName == "call" || n.MemberName == "staticcall" || n.MemberName == "callcode") &&
			n.Expression != nil && typeFromDescriptions(n.Expression.TypeDescriptions).Kind == "address" {
			h := newHole("UNSUPPORTED_LOW_LEVEL_CALL", extractSnippet(src.Content, n.Src), fnScopeName(fn))
			recordHole(c, fn, h)
			return &HoleExpr{Hole: h, Type: typ}
		}
		return &MemberExpr{
			Object: lowerExpr(src, c, fn, n.Expression),
			Member: n.MemberName,
			Type:   typ,
		}

	case "IndexAccess":
		return &IndexExpr{
			Base:  lowerExpr(src, c, fn, n.BaseExpression),
			Index: lowerExpr(src, c, fn, n.IndexExpression),
			Type:  typ,
		}

	case "BinaryOperation":
		return &BinaryExpr{
			Op:    n.Operator,
			Left:  lowerExpr(src, c, fn, n.LeftExpression),
			Right: lowerExpr(src, c, fn, n.RightExpression),
			Type:  typ,
		}

	case "UnaryOperation":
		return &UnaryExpr{
			Op:      n.Operator,
			Prefix:  true,
			Operand: lowerExpr(src, c, fn, n.SubExpression),
			Type:    typ,
		}

	case "FunctionCall":
		// Si alguna subexpresión es un HoleExpr (p.ej. target.delegatecall(...)), elevar el call entero a HoleExpr
		callee := lowerExpr(src, c, fn, n.Expression)
		if he, ok := callee.(*HoleExpr); ok {
			he.Hole.OriginalSolidity = extractSnippet(src.Content, n.Src)
			return he
		}
		ce := &CallExpr{
			Callee:       callee,
			IsTypeCast:   n.Kind == "typeConversion",
			IsStructCtor: n.Kind == "structConstructorCall",
			Type:         typ,
		}
		if ce.IsTypeCast {
			ce.CastTarget = typ
		}
		for _, a := range n.Arguments {
			ce.Args = append(ce.Args, lowerExpr(src, c, fn, a))
		}
		return ce

	case "Conditional":
		return &TernaryExpr{
			Cond:      lowerExpr(src, c, fn, n.Condition),
			TrueExpr:  lowerExpr(src, c, fn, n.TrueExpression),
			FalseExpr: lowerExpr(src, c, fn, n.FalseExpression),
			Type:      typ,
		}

	case "TupleExpression":
		if len(n.Components) == 1 && n.Components[0] != nil {
			return lowerExpr(src, c, fn, n.Components[0])
		}
		te := &TupleExpr{Type: typ}
		for _, comp := range n.Components {
			te.Components = append(te.Components, lowerExpr(src, c, fn, comp))
		}
		return te

	case "ElementaryTypeNameExpression":
		t := lowerTypeName(n.TypeName, n.TypeDescriptions)
		return &IdentExpr{Name: t.Kind, Type: t}

	case "NewExpression":
		h := newHole("UNSUPPORTED_CREATE", extractSnippet(src.Content, n.Src), fnScopeName(fn))
		recordHole(c, fn, h)
		return &HoleExpr{Hole: h, Type: typ}

	default:
		h := newHole("UNSUPPORTED_EXPR_"+n.NodeType, extractSnippet(src.Content, n.Src), fnScopeName(fn))
		recordHole(c, fn, h)
		return &HoleExpr{Hole: h, Type: typ}
	}
}

func lowerParams(ns *solc.NodeOrSlice) []Param {
	nodes := ns.ParamNodes()
	out := make([]Param, 0, len(nodes))
	for _, p := range nodes {
		out = append(out, Param{
			Name: p.Name,
			Type: lowerTypeName(p.TypeName, p.TypeDescriptions),
		})
	}
	return out
}

func lowerTypeName(tn *solc.Node, td *solc.TypeDescriptions) TypeRef {
	if tn == nil {
		return typeFromDescriptions(td)
	}
	switch tn.NodeType {
	case "ElementaryTypeName":
		return parseElementaryType(tn.Name)
	case "Mapping":
		kt := lowerTypeName(tn.KeyType, tn.KeyType.TypeDescriptions)
		vt := lowerTypeName(tn.ValueType, tn.ValueType.TypeDescriptions)
		return TypeRef{Kind: "mapping", KeyType: &kt, ValueType: &vt}
	case "ArrayTypeName":
		et := lowerTypeName(tn.BaseType, tn.BaseType.TypeDescriptions)
		return TypeRef{Kind: "array", ElemType: &et}
	case "UserDefinedTypeName":
		t := typeFromDescriptions(td)
		if t.Name == "" {
			t.Name = tn.Name
		}
		if t.Kind == "" || t.Kind == "unknown" {
			t.Kind = "struct"
		}
		return t
	default:
		return typeFromDescriptions(td)
	}
}

func parseElementaryType(name string) TypeRef {
	switch {
	case name == "bool":
		return TypeRef{Kind: "bool"}
	case name == "string":
		return TypeRef{Kind: "string"}
	case name == "address" || name == "address payable":
		return TypeRef{Kind: "address"}
	case name == "bytes":
		return TypeRef{Kind: "bytes"}
	case strings.HasPrefix(name, "bytes"):
		n, _ := strconv.Atoi(strings.TrimPrefix(name, "bytes"))
		return TypeRef{Kind: "bytes", BitSize: n}
	case name == "uint":
		return TypeRef{Kind: "uint", BitSize: 256}
	case strings.HasPrefix(name, "uint"):
		bits, _ := strconv.Atoi(strings.TrimPrefix(name, "uint"))
		return TypeRef{Kind: "uint", BitSize: bits}
	case name == "int":
		return TypeRef{Kind: "int", BitSize: 256}
	case strings.HasPrefix(name, "int"):
		bits, _ := strconv.Atoi(strings.TrimPrefix(name, "int"))
		return TypeRef{Kind: "int", BitSize: bits}
	default:
		return TypeRef{Kind: "unknown", Name: name}
	}
}

func typeFromDescriptions(td *solc.TypeDescriptions) TypeRef {
	if td == nil {
		return TypeRef{Kind: "unknown"}
	}
	ts := strings.TrimSpace(td.TypeString)
	ti := strings.TrimSpace(td.TypeIdentifier)

	switch {
	case ts == "bool" || ti == "t_bool":
		return TypeRef{Kind: "bool"}
	case strings.HasPrefix(ts, "string") || strings.HasPrefix(ti, "t_string"):
		return TypeRef{Kind: "string"}
	case strings.HasPrefix(ts, "address") || strings.HasPrefix(ti, "t_address"):
		return TypeRef{Kind: "address"}
	case strings.HasPrefix(ts, "uint") || strings.HasPrefix(ti, "t_uint"):
		clean := strings.Split(ts, " ")[0]
		if strings.HasPrefix(clean, "uint") {
			return parseElementaryType(clean)
		}
		return TypeRef{Kind: "uint", BitSize: 256}
	case strings.HasPrefix(ts, "int_const"):
		return TypeRef{Kind: "int_const"}
	case strings.HasPrefix(ts, "int") || strings.HasPrefix(ti, "t_int"):
		clean := strings.Split(ts, " ")[0]
		if strings.HasPrefix(clean, "int") {
			return parseElementaryType(clean)
		}
		return TypeRef{Kind: "int", BitSize: 256}
	case strings.HasPrefix(ts, "bytes") || strings.HasPrefix(ti, "t_bytes"):
		clean := strings.Split(ts, " ")[0]
		return parseElementaryType(clean)
	case strings.HasPrefix(ts, "struct "):
		parts := strings.Fields(ts)
		name := parts[1]
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			name = name[dot+1:]
		}
		return TypeRef{Kind: "struct", Name: name}
	case strings.HasPrefix(ts, "enum "):
		parts := strings.Fields(ts)
		name := parts[1]
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			name = name[dot+1:]
		}
		return TypeRef{Kind: "enum", Name: name}
	case strings.HasPrefix(ts, "contract "):
		parts := strings.Fields(ts)
		return TypeRef{Kind: "contract", Name: parts[1]}
	case strings.Contains(ts, "[]"):
		baseStr := strings.Split(ts, "[]")[0]
		elem := typeFromDescriptions(&solc.TypeDescriptions{TypeString: baseStr})
		return TypeRef{Kind: "array", ElemType: &elem}
	default:
		return TypeRef{Kind: "unknown", Name: ts}
	}
}

func extractSnippet(content, src string) string {
	offset, length, _, ok := solc.ParseSrcSpan(src)
	if !ok || offset < 0 || offset >= len(content) {
		return ""
	}
	end := offset + length
	if end > len(content) {
		end = len(content)
	}
	return strings.TrimSpace(content[offset:end])
}

func newHole(reason, snippet, scope string) *Hole {
	sum := sha256.Sum256([]byte(reason + "\n" + scope + "\n" + snippet))
	return &Hole{
		ID:               hex.EncodeToString(sum[:6]),
		Reason:           reason,
		OriginalSolidity: snippet,
		ScopeSummary:     scope,
	}
}

func recordHole(c *Contract, fn *Function, h *Hole) {
	if c != nil {
		c.Holes = append(c.Holes, h)
	}
	if fn != nil {
		fn.Holes = append(fn.Holes, h)
	}
}

func fnScopeName(fn *Function) string {
	if fn == nil {
		return "contract"
	}
	if fn.Name != "" {
		return fn.Name
	}
	return fn.Kind
}
