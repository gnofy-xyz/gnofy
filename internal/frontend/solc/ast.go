package solc

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// NodeOrSlice deserializa campos del AST de solc que pueden ser un objeto (p.ej. ParameterList)
// o un array de nodos (p.ej. los parámetros dentro de ParameterList).
type NodeOrSlice struct {
	Node  *Node
	Slice []*Node
}

// UnmarshalJSON soporta tanto un objeto JSON como un array JSON.
func (ns *NodeOrSlice) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '[' {
		return json.Unmarshal(trimmed, &ns.Slice)
	}
	var n Node
	if err := json.Unmarshal(trimmed, &n); err != nil {
		return err
	}
	ns.Node = &n
	return nil
}

// ParamNodes devuelve los nodos parámetro si el campo contiene una ParameterList o un slice directo.
func (ns *NodeOrSlice) ParamNodes() []*Node {
	if ns == nil {
		return nil
	}
	if len(ns.Slice) > 0 {
		return ns.Slice
	}
	if ns.Node != nil && ns.Node.Parameters != nil {
		return ns.Node.Parameters.Slice
	}
	return nil
}

// StringOrNode deserializa el campo "value" del AST de solc, que es string en Literal
// y un objeto *Node en VariableDeclaration (valor inicial) o YulAssignment.
type StringOrNode struct {
	String string
	Node   *Node
}

// UnmarshalJSON soporta tanto un string JSON como un objeto JSON.
func (sn *StringOrNode) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		return json.Unmarshal(trimmed, &sn.String)
	}
	if trimmed[0] == '{' {
		var n Node
		if err := json.Unmarshal(trimmed, &n); err != nil {
			return err
		}
		sn.Node = &n
		return nil
	}
	return nil
}

// TypeDescriptions contiene los metadatos de tipo resueltos por solc.
type TypeDescriptions struct {
	TypeIdentifier string `json:"typeIdentifier"`
	TypeString     string `json:"typeString"`
}

// Node representa cualquier nodo del AST compacto JSON emitido por solc.
type Node struct {
	ID       int    `json:"id"`
	NodeType string `json:"nodeType"`
	Src      string `json:"src"`

	// Identidad y metadatos comunes
	Name            string   `json:"name"`
	AbsolutePath    string   `json:"absolutePath"`
	File            string   `json:"file"`
	ContractKind    string   `json:"contractKind"`
	Abstract        bool     `json:"abstract"`
	Kind            string   `json:"kind"`
	Visibility      string   `json:"visibility"`
	StateMutability string   `json:"stateMutability"`
	Operator        string       `json:"operator"`
	MemberName      string       `json:"memberName"`
	Value           StringOrNode `json:"value"`
	HexValue        string       `json:"hexValue"`
	Constant        bool     `json:"constant"`
	Immutable       bool     `json:"immutable"`
	StateVariable   bool     `json:"stateVariable"`
	Virtual         bool     `json:"virtual"`
	Implemented     bool     `json:"implemented"`
	StorageLocation string   `json:"storageLocation"`
	Literals        []string `json:"literals"`
	Flags           []string `json:"flags"`
	EVMVersion      string   `json:"evmVersion"`

	TypeDescriptions *TypeDescriptions `json:"typeDescriptions"`

	// Hijos estructurales
	Nodes                    []*Node      `json:"nodes"`
	Statements               []*Node      `json:"statements"`
	Body                     *Node        `json:"body"`
	Parameters               *NodeOrSlice `json:"parameters"`
	ReturnParameters         *NodeOrSlice `json:"returnParameters"`
	Modifiers                []*Node      `json:"modifiers"`
	ModifierName             *Node        `json:"modifierName"`
	BaseContracts            []*Node      `json:"baseContracts"`
	BaseName                 *Node        `json:"baseName"`
	TypeName                 *Node        `json:"typeName"`
	KeyType                  *Node        `json:"keyType"`
	ValueType                *Node        `json:"valueType"`
	BaseType                 *Node        `json:"baseType"`
	Expression               *Node        `json:"expression"`
	LeftExpression           *Node        `json:"leftExpression"`
	RightExpression          *Node        `json:"rightExpression"`
	SubExpression            *Node        `json:"subExpression"`
	Arguments                []*Node      `json:"arguments"`
	Components               []*Node      `json:"components"`
	Declarations             []*Node      `json:"declarations"`
	InitialValue             *Node        `json:"initialValue"`
	Condition                *Node        `json:"condition"`
	TrueBody                 *Node        `json:"trueBody"`
	FalseBody                *Node        `json:"falseBody"`
	TrueExpression           *Node        `json:"trueExpression"`
	FalseExpression          *Node        `json:"falseExpression"`
	InitializationExpression *Node        `json:"initializationExpression"`
	LoopExpression           *Node        `json:"loopExpression"`
	EventCall                *Node        `json:"eventCall"`
	ErrorCall                *Node        `json:"errorCall"`
	ExternalCall             *Node        `json:"externalCall"`
	Clauses                  []*Node      `json:"clauses"`
	LeftHandSide             *Node        `json:"leftHandSide"`
	RightHandSide            *Node        `json:"rightHandSide"`
	BaseExpression           *Node        `json:"baseExpression"`
	IndexExpression          *Node        `json:"indexExpression"`
	Length                   *Node        `json:"length"`
	Overrides                *NodeOrSlice `json:"overrides"`
	Members                  []*Node      `json:"members"`
	AST                      *Node        `json:"AST"`
}

// Walk recorre el AST en preorden. Si fn devuelve false para un nodo,
// no se desciende a sus hijos.
func Walk(root *Node, fn func(*Node) bool) {
	if root == nil || !fn(root) {
		return
	}
	walkSlice(root.Nodes, fn)
	walkSlice(root.BaseContracts, fn)
	Walk(root.BaseName, fn)
	Walk(root.TypeName, fn)
	Walk(root.KeyType, fn)
	Walk(root.ValueType, fn)
	Walk(root.BaseType, fn)
	Walk(root.Length, fn)
	walkNodeOrSlice(root.Parameters, fn)
	walkNodeOrSlice(root.ReturnParameters, fn)
	walkSlice(root.Modifiers, fn)
	Walk(root.ModifierName, fn)
	walkNodeOrSlice(root.Overrides, fn)
	walkSlice(root.Members, fn)
	Walk(root.InitialValue, fn)
	Walk(root.Value.Node, fn)
	Walk(root.Body, fn)
	walkSlice(root.Statements, fn)
	Walk(root.InitializationExpression, fn)
	Walk(root.Condition, fn)
	Walk(root.LoopExpression, fn)
	Walk(root.TrueBody, fn)
	Walk(root.FalseBody, fn)
	Walk(root.TrueExpression, fn)
	Walk(root.FalseExpression, fn)
	Walk(root.Expression, fn)
	Walk(root.LeftExpression, fn)
	Walk(root.RightExpression, fn)
	Walk(root.SubExpression, fn)
	Walk(root.LeftHandSide, fn)
	Walk(root.RightHandSide, fn)
	Walk(root.BaseExpression, fn)
	Walk(root.IndexExpression, fn)
	walkSlice(root.Arguments, fn)
	walkSlice(root.Components, fn)
	walkSlice(root.Declarations, fn)
	Walk(root.EventCall, fn)
	Walk(root.ErrorCall, fn)
	Walk(root.ExternalCall, fn)
	walkSlice(root.Clauses, fn)
}

func walkSlice(nodes []*Node, fn func(*Node) bool) {
	for _, n := range nodes {
		Walk(n, fn)
	}
}

func walkNodeOrSlice(ns *NodeOrSlice, fn func(*Node) bool) {
	if ns == nil {
		return
	}
	if ns.Node != nil {
		Walk(ns.Node, fn)
	}
	walkSlice(ns.Slice, fn)
}

// ParseSrcSpan extrae offset, length y fileIndex de un campo src "offset:length:fileIdx".
func ParseSrcSpan(src string) (offset, length, fileIndex int, ok bool) {
	parts := strings.Split(src, ":")
	if len(parts) < 3 {
		return 0, 0, 0, false
	}
	o, err1 := strconv.Atoi(parts[0])
	l, err2 := strconv.Atoi(parts[1])
	f, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return o, l, f, true
}

// OffsetToLineCol convierte un offset en bytes a línea y columna (1-based).
func OffsetToLineCol(content string, offset int) (line, col int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}
	line = 1
	col = 1
	for i := 0; i < offset; i++ {
		if content[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// SrcLocation formatea una ubicación "archivo:linea:col" a partir del campo src y el contenido fuente.
func SrcLocation(file, content, src string) string {
	offset, _, _, ok := ParseSrcSpan(src)
	if !ok || content == "" {
		return file
	}
	line, col := OffsetToLineCol(content, offset)
	return file + ":" + strconv.Itoa(line) + ":" + strconv.Itoa(col)
}
