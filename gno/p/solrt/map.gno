package solrt

// OrderedMap es un mapa clave-valor determinista en Gno puro (Go 1.17)
// respaldado por un árbol binario balanceado (AVL) para representar
// mappings de Solidity con orden determinista y sin dependencias externas.
type OrderedMap struct {
	root *avlNode
	size int
}

type avlNode struct {
	key    string
	val    any
	height int
	left   *avlNode
	right  *avlNode
}

// NewOrderedMap crea un OrderedMap vacío.
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{}
}

// Size devuelve el número de entradas almacenadas.
func (m *OrderedMap) Size() int {
	if m == nil {
		return 0
	}
	return m.size
}

// Get busca key y devuelve (valor, true) si existe o (nil, false) si no.
func (m *OrderedMap) Get(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	cur := m.root
	for cur != nil {
		if key == cur.key {
			return cur.val, true
		}
		if key < cur.key {
			cur = cur.left
		} else {
			cur = cur.right
		}
	}
	return nil, false
}

// Set inserta o actualiza key con val.
func (m *OrderedMap) Set(key string, val any) {
	var inserted bool
	m.root, inserted = avlInsert(m.root, key, val)
	if inserted {
		m.size++
	}
}

// GetU256 devuelve el U256 asociado a key o U256Zero() si no existe (semántica Solidity).
func (m *OrderedMap) GetU256(key string) U256 {
	v, ok := m.Get(key)
	if !ok || v == nil {
		return U256Zero()
	}
	return v.(U256)
}

// SetU256 guarda un U256 bajo key.
func (m *OrderedMap) SetU256(key string, val U256) {
	m.Set(key, val)
}

// GetU64 devuelve el uint64 asociado a key o 0 si no existe.
func (m *OrderedMap) GetU64(key string) uint64 {
	v, ok := m.Get(key)
	if !ok || v == nil {
		return 0
	}
	return v.(uint64)
}

// SetU64 guarda un uint64 bajo key.
func (m *OrderedMap) SetU64(key string, val uint64) {
	m.Set(key, val)
}

// GetBool devuelve el bool asociado a key o false si no existe.
func (m *OrderedMap) GetBool(key string) bool {
	v, ok := m.Get(key)
	if !ok || v == nil {
		return false
	}
	return v.(bool)
}

// SetBool guarda un bool bajo key.
func (m *OrderedMap) SetBool(key string, val bool) {
	m.Set(key, val)
}

// GetString devuelve el string asociado a key o "" si no existe.
func (m *OrderedMap) GetString(key string) string {
	v, ok := m.Get(key)
	if !ok || v == nil {
		return ""
	}
	return v.(string)
}

// SetString guarda un string bajo key.
func (m *OrderedMap) SetString(key string, val string) {
	m.Set(key, val)
}

func avlHeight(n *avlNode) int {
	if n == nil {
		return 0
	}
	return n.height
}

func avlUpdateHeight(n *avlNode) {
	lh := avlHeight(n.left)
	rh := avlHeight(n.right)
	if lh > rh {
		n.height = lh + 1
	} else {
		n.height = rh + 1
	}
}

func avlBalance(n *avlNode) int {
	if n == nil {
		return 0
	}
	return avlHeight(n.left) - avlHeight(n.right)
}

func avlRotateRight(y *avlNode) *avlNode {
	x := y.left
	t2 := x.right
	x.right = y
	y.left = t2
	avlUpdateHeight(y)
	avlUpdateHeight(x)
	return x
}

func avlRotateLeft(x *avlNode) *avlNode {
	y := x.right
	t2 := y.left
	y.left = x
	x.right = t2
	avlUpdateHeight(x)
	avlUpdateHeight(y)
	return y
}

func avlInsert(n *avlNode, key string, val any) (*avlNode, bool) {
	if n == nil {
		return &avlNode{key: key, val: val, height: 1}, true
	}
	var inserted bool
	if key < n.key {
		n.left, inserted = avlInsert(n.left, key, val)
	} else if key > n.key {
		n.right, inserted = avlInsert(n.right, key, val)
	} else {
		n.val = val
		return n, false
	}

	avlUpdateHeight(n)
	bal := avlBalance(n)

	if bal > 1 && key < n.left.key {
		return avlRotateRight(n), inserted
	}
	if bal < -1 && key > n.right.key {
		return avlRotateLeft(n), inserted
	}
	if bal > 1 && key > n.left.key {
		n.left = avlRotateLeft(n.left)
		return avlRotateRight(n), inserted
	}
	if bal < -1 && key < n.right.key {
		n.right = avlRotateRight(n.right)
		return avlRotateLeft(n), inserted
	}
	return n, inserted
}
