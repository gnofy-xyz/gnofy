package passes

import (
	"github.com/gnofy-xyz/gnofy/internal/ir"
)

// CrossingPass determina qué funciones son puntos de entrada crossing (`cur realm`)
// y calcula exactamente qué paquetes runtime/chain necesita importar cada contrato.
type CrossingPass struct{}

func (CrossingPass) Name() string { return "crossing" }

func (CrossingPass) Run(mod *ir.Module) error {
	for _, c := range mod.Contracts {
		for _, sv := range c.StateVars {
			if sv.Type.IsU256() || sv.Type.Kind == "mapping" {
				c.UsesSolrt = true
			}
		}

		for _, fn := range c.Functions {
			if fn.IsInit && len(fn.Params) > 0 {
				c.HasInitArgs = true
			}

			var fnFlags fnScanFlags
			scanStmts(c, fn.Body, &fnFlags)

			// Las funciones públicas/externas que mutan estado o consultan msg.sender
			// (y el constructor parametrizado Init) llevan `cur realm` en Gno.
			if fn.IsInit && len(fn.Params) > 0 {
				fn.IsCrossing = true
			} else if (fn.Visibility == ir.VisPublic || fn.Visibility == ir.VisExternal) && (!fn.ViewOrPure || fnFlags.usesMsgSender) {
				fn.IsCrossing = true
			}

			// Solo necesitamos chain/runtime/unsafe si msg.sender o address(this) se usan
			// fuera de una función crossing (donde existe `cur realm`).
			if !fn.IsCrossing && (fnFlags.usesMsgSender || fnFlags.usesAddressThis) {
				c.UsesRuntimeUnsafe = true
			}
		}
	}
	return nil
}

type fnScanFlags struct {
	usesMsgSender   bool
	usesAddressThis bool
}

func scanStmts(c *ir.Contract, stmts []ir.Stmt, flags *fnScanFlags) {
	for _, s := range stmts {
		switch st := s.(type) {
		case *ir.BlockStmt:
			scanStmts(c, st.Stmts, flags)
		case *ir.UncheckedStmt:
			c.UsesSolrt = true
			scanStmts(c, st.Body, flags)
		case *ir.VarDeclStmt:
			for _, v := range st.Vars {
				if v.Type.IsU256() {
					c.UsesSolrt = true
				}
			}
			scanExpr(c, st.Init, flags)
		case *ir.AssignStmt:
			if st.Op != "=" {
				c.UsesSolrt = true
			}
			scanExpr(c, st.LHS, flags)
			scanExpr(c, st.RHS, flags)
		case *ir.ExprStmt:
			scanExpr(c, st.Expr, flags)
		case *ir.IfStmt:
			scanExpr(c, st.Cond, flags)
			scanStmts(c, st.Then, flags)
			scanStmts(c, st.Else, flags)
		case *ir.ForStmt:
			if st.Init != nil {
				scanStmts(c, []ir.Stmt{st.Init}, flags)
			}
			scanExpr(c, st.Cond, flags)
			if st.Post != nil {
				scanStmts(c, []ir.Stmt{st.Post}, flags)
			}
			scanStmts(c, st.Body, flags)
		case *ir.WhileStmt:
			scanExpr(c, st.Cond, flags)
			scanStmts(c, st.Body, flags)
		case *ir.ReturnStmt:
			scanExpr(c, st.Value, flags)
		case *ir.EmitStmt:
			c.UsesChainEmit = true
			c.UsesSolrt = true
			for _, a := range st.Args {
				scanExpr(c, a, flags)
			}
		case *ir.RevertStmt:
			c.UsesSolrt = true
			for _, a := range st.Args {
				scanExpr(c, a, flags)
			}
		}
	}
}

func scanExpr(c *ir.Contract, e ir.Expr, flags *fnScanFlags) {
	if e == nil {
		return
	}
	switch ex := e.(type) {
	case *ir.MemberExpr:
		if id, ok := ex.Object.(*ir.IdentExpr); ok {
			if id.Name == "msg" && ex.Member == "sender" {
				flags.usesMsgSender = true
			}
			if id.Name == "msg" && ex.Member == "value" {
				c.UsesBanker = true
			}
			if id.Name == "tx" && ex.Member == "origin" {
				c.UsesRuntimeUnsafe = true
			}
			if id.Name == "block" && ex.Member == "number" {
				c.UsesRuntime = true
			}
			if id.Name == "SafeERC20" || id.Name == "PRBMath" {
				c.UsesSolrt = true
			}
		}
		if ex.Member == "balance" && ex.Object != nil && ex.Object.ExprType().Kind == "address" {
			c.UsesBanker = true
			c.UsesSolrt = true
		}
		scanExpr(c, ex.Object, flags)
	case *ir.IndexExpr:
		scanExpr(c, ex.Base, flags)
		scanExpr(c, ex.Index, flags)
	case *ir.BinaryExpr:
		if ex.Op == "+" || ex.Op == "-" || ex.Op == "*" || ex.Op == "/" || ex.Op == "%" || ex.Op == "**" {
			c.UsesSolrt = true
		}
		scanExpr(c, ex.Left, flags)
		scanExpr(c, ex.Right, flags)
	case *ir.UnaryExpr:
		scanExpr(c, ex.Operand, flags)
	case *ir.CallExpr:
		if id, ok := ex.Callee.(*ir.IdentExpr); ok {
			if id.Name == "require" || id.Name == "assert" || id.Name == "revert" {
				c.UsesSolrt = true
			}
		}
		if ex.IsTypeCast && ex.CastTarget.Kind == "contract" && ex.CastTarget.Name == "IERC20" {
			c.UsesSolrt = true
		}
		if ex.IsTypeCast && ex.CastTarget.Kind == "address" && len(ex.Args) == 1 {
			if id, ok := ex.Args[0].(*ir.IdentExpr); ok && id.Name == "this" {
				flags.usesAddressThis = true
			}
		}
		scanExpr(c, ex.Callee, flags)
		for _, a := range ex.Args {
			scanExpr(c, a, flags)
		}
	case *ir.TernaryExpr:
		scanExpr(c, ex.Cond, flags)
		scanExpr(c, ex.TrueExpr, flags)
		scanExpr(c, ex.FalseExpr, flags)
	case *ir.TupleExpr:
		for _, comp := range ex.Components {
			scanExpr(c, comp, flags)
		}
	}
}
