// Package ir define la Representación Intermedia (IR) agnóstica entre el AST
// de Solidity y el emisor de código Gno (Go 1.17 + stdlib Gno).
package ir

// Module agrupa las unidades compiladas a partir de un conjunto de fuentes Solidity.
type Module struct {
	Name      string
	Contracts []*Contract
}

// ContractKind distingue entre contratos desplegables (Realms), librerías e interfaces.
type ContractKind string

const (
	KindContract  ContractKind = "contract"
	KindLibrary   ContractKind = "library"
	KindInterface ContractKind = "interface"
)

// Visibility refleja la visibilidad declarada en Solidity.
type Visibility string

const (
	VisPublic   Visibility = "public"
	VisExternal Visibility = "external"
	VisInternal Visibility = "internal"
	VisPrivate  Visibility = "private"
)

// Contract representa un contrato, librería o interfaz en la IR.
type Contract struct {
	Name         string
	GoPkgName    string
	Kind         ContractKind
	Abstract     bool
	BaseNames    []string
	BaseCtorArgs map[string][]Expr
	Enums        []*EnumDef
	Structs      []*StructDef
	Events       []*EventDef
	Errors       []*ErrorDef
	StateVars    []*StateVar
	Modifiers    []*ModifierDef
	Functions    []*Function
	Holes        []*Hole
	SourcePath   string

	// Metadatos rellenados por pases (crossing / naming / standards)
	UsesReentrancyGuard bool
	UsesRuntime         bool
	UsesRuntimeUnsafe   bool
	UsesChainEmit       bool
	UsesBanker          bool
	UsesSolrt           bool
	HasInitArgs         bool

	// Metadatos del modo --idiomatic (StandardsPass)
	IdiomaticGRC20   bool
	IdiomaticOwnable bool
	HasMint          bool
	HasBurn          bool
	DecimalsDefault  int
}

// EnumDef representa un enum de Solidity.
type EnumDef struct {
	Name    string
	GoName  string
	Members []string
}

// StructDef representa un struct de Solidity.
type StructDef struct {
	Name   string
	GoName string
	Fields []Param
}

// EventDef representa una declaración event de Solidity.
type EventDef struct {
	Name   string
	Params []Param
}

// ErrorDef representa un custom error de Solidity.
type ErrorDef struct {
	Name   string
	Params []Param
}

// StateVar representa una variable de estado persistente.
type StateVar struct {
	Name         string
	GoName       string
	GetterGoName string
	Type         TypeRef
	Visibility   Visibility
	Constant     bool
	Immutable    bool
	InitialValue Expr
}

// ModifierDef representa un modifier de Solidity antes de su expansión.
type ModifierDef struct {
	Name   string
	Params []Param
	Body   []Stmt
}

// ModifierCall representa la aplicación de un modifier a una función.
type ModifierCall struct {
	Name string
	Args []Expr
}

// Function representa una función, constructor, fallback o receive.
type Function struct {
	Name        string
	GoName      string
	Kind        string // "constructor", "function", "fallback", "receive"
	IsInit      bool
	IsCrossing  bool
	IsGetter    bool
	Virtual     bool
	IsOverride  bool
	Payable     bool
	ViewOrPure  bool
	Visibility  Visibility
	Params      []Param
	Returns     []Param
	Modifiers   []ModifierCall
	Body        []Stmt
	Holes       []*Hole
}

// Param representa un parámetro o campo tipado.
type Param struct {
	Name   string
	GoName string
	Type   TypeRef
}

// TypeRef normaliza un tipo Solidity hacia el modelo de tipos de la IR.
type TypeRef struct {
	Kind      string // bool, string, bytes, uint, int, address, array, mapping, struct, enum, contract, void, unknown
	BitSize   int    // 8, 64, 128, 256, etc.
	Name      string // nombre de struct, enum o contrato si aplica
	KeyType   *TypeRef
	ValueType *TypeRef
	ElemType  *TypeRef
	ArrayLen  int // 0 si es dinámico
}

// IsU256 indica si el tipo requiere aritmética de 256 bits (solrt.U256).
func (t TypeRef) IsU256() bool {
	return t.Kind == "uint" && t.BitSize > 64
}

// IsU64OrSmaller indica si cabe en uint64 nativo con helpers checked.
func (t TypeRef) IsU64OrSmaller() bool {
	return t.Kind == "uint" && t.BitSize > 0 && t.BitSize <= 64
}

// IsSigned64OrSmaller indica si es un entero con signo soportado (int8..int64).
func (t TypeRef) IsSigned64OrSmaller() bool {
	return t.Kind == "int" && t.BitSize > 0 && t.BitSize <= 64
}

// IsWideSigned indica si es un entero con signo >64 bits (int72..int256) sin equivalencia exacta todavía.
func (t TypeRef) IsWideSigned() bool {
	return t.Kind == "int" && t.BitSize > 64
}

// ContainsWideSigned inspecciona recursivamente si el tipo (o sus elementos/valores) usa intN con N > 64.
func (t TypeRef) ContainsWideSigned() bool {
	if t.IsWideSigned() {
		return true
	}
	if t.KeyType != nil && t.KeyType.ContainsWideSigned() {
		return true
	}
	if t.ValueType != nil && t.ValueType.ContainsWideSigned() {
		return true
	}
	if t.ElemType != nil && t.ElemType.ContainsWideSigned() {
		return true
	}
	return false
}

// Hole representa un fragmento sin traducción determinista 1:1.
type Hole struct {
	ID               string
	Reason           string
	OriginalSolidity string
	ScopeSummary     string
	ResolvedGno      string
	Verified         bool
}

// Stmt es la interfaz común para sentencias en la IR.
type Stmt interface {
	stmtNode()
}

// Expr es la interfaz común para expresiones en la IR.
type Expr interface {
	exprNode()
	ExprType() TypeRef
}

// --- Sentencias ---

type BlockStmt struct {
	Stmts []Stmt
}

type VarDeclStmt struct {
	Vars       []Param
	IsStorage  bool
	Init       Expr
}

type AssignStmt struct {
	Op  string // "=", "+=", "-=", "*=", "/=", "%="
	LHS Expr
	RHS Expr
}

type ExprStmt struct {
	Expr Expr
}

type IfStmt struct {
	Cond Expr
	Then []Stmt
	Else []Stmt
}

type ForStmt struct {
	Init Stmt
	Cond Expr
	Post Stmt
	Body []Stmt
}

type WhileStmt struct {
	Cond Expr
	Body []Stmt
}

type ReturnStmt struct {
	Value Expr
}

type EmitStmt struct {
	EventName string
	Args      []Expr
}

type RevertStmt struct {
	ErrorName string
	Args      []Expr
}

type UncheckedStmt struct {
	Body []Stmt
}

type BreakStmt struct{}
type ContinueStmt struct{}
type PlaceholderStmt struct{}

type HoleStmt struct {
	Hole *Hole
}

func (*BlockStmt) stmtNode()       {}
func (*VarDeclStmt) stmtNode()     {}
func (*AssignStmt) stmtNode()      {}
func (*ExprStmt) stmtNode()        {}
func (*IfStmt) stmtNode()          {}
func (*ForStmt) stmtNode()         {}
func (*WhileStmt) stmtNode()       {}
func (*ReturnStmt) stmtNode()      {}
func (*EmitStmt) stmtNode()        {}
func (*RevertStmt) stmtNode()      {}
func (*UncheckedStmt) stmtNode()   {}
func (*BreakStmt) stmtNode()       {}
func (*ContinueStmt) stmtNode()    {}
func (*PlaceholderStmt) stmtNode() {}
func (*HoleStmt) stmtNode()        {}

// --- Expresiones ---

type LiteralExpr struct {
	Kind  string // "number", "string", "bool", "address"
	Value string
	Type  TypeRef
}

type IdentExpr struct {
	Name string
	Type TypeRef
}

type MemberExpr struct {
	Object Expr
	Member string
	Type   TypeRef
}

type IndexExpr struct {
	Base  Expr
	Index Expr
	Type  TypeRef
}

type BinaryExpr struct {
	Op    string
	Left  Expr
	Right Expr
	Type  TypeRef
}

type UnaryExpr struct {
	Op      string
	Prefix  bool
	Operand Expr
	Type    TypeRef
}

type CallExpr struct {
	Callee       Expr
	Args         []Expr
	IsTypeCast   bool
	IsStructCtor bool
	CastTarget   TypeRef
	Type         TypeRef
}

type TernaryExpr struct {
	Cond      Expr
	TrueExpr  Expr
	FalseExpr Expr
	Type      TypeRef
}

type TupleExpr struct {
	Components []Expr
	Type       TypeRef
}

type HoleExpr struct {
	Hole *Hole
	Type TypeRef
}

func (*LiteralExpr) exprNode() {}
func (*IdentExpr) exprNode()   {}
func (*MemberExpr) exprNode()  {}
func (*IndexExpr) exprNode()   {}
func (*BinaryExpr) exprNode()  {}
func (*UnaryExpr) exprNode()   {}
func (*CallExpr) exprNode()    {}
func (*TernaryExpr) exprNode() {}
func (*TupleExpr) exprNode()   {}
func (*HoleExpr) exprNode()    {}

func (e *LiteralExpr) ExprType() TypeRef { return e.Type }
func (e *IdentExpr) ExprType() TypeRef   { return e.Type }
func (e *MemberExpr) ExprType() TypeRef  { return e.Type }
func (e *IndexExpr) ExprType() TypeRef   { return e.Type }
func (e *BinaryExpr) ExprType() TypeRef  { return e.Type }
func (e *UnaryExpr) ExprType() TypeRef   { return e.Type }
func (e *CallExpr) ExprType() TypeRef    { return e.Type }
func (e *TernaryExpr) ExprType() TypeRef { return e.Type }
func (e *TupleExpr) ExprType() TypeRef   { return e.Type }
func (e *HoleExpr) ExprType() TypeRef    { return e.Type }
