// Package solrt provee primitivas de runtime en Gno puro (estrictamente Go 1.17)
// para preservar la semántica de Solidity (aritmética checked, require/revert/assert).
package solrt

import "strconv"

// Require equivale a require(cond, msg) en Solidity.
func Require(cond bool, msg string) {
	if !cond {
		if msg == "" {
			panic("solrt: require failed")
		}
		panic(msg)
	}
}

// Assert equivale a assert(cond) en Solidity.
func Assert(cond bool) {
	if !cond {
		panic("solrt: assert failed")
	}
}

// Revert equivale a revert(msg) en Solidity.
func Revert(msg string) {
	if msg == "" {
		panic("solrt: revert")
	}
	panic(msg)
}

// --- Aritmética unsigned checked exacta por ancho (Solidity >=0.8) ---

func SafeAddU8(a, b uint8) uint8 {
	c := a + b
	if c < a {
		panic("solrt: uint8 addition overflow")
	}
	return c
}

func SafeSubU8(a, b uint8) uint8 {
	if b > a {
		panic("solrt: uint8 subtraction underflow")
	}
	return a - b
}

func SafeMulU8(a, b uint8) uint8 {
	c := uint16(a) * uint16(b)
	if c > 0xff {
		panic("solrt: uint8 multiplication overflow")
	}
	return uint8(c)
}

func SafeDivU8(a, b uint8) uint8 {
	if b == 0 {
		panic("solrt: division by zero")
	}
	return a / b
}

func SafeModU8(a, b uint8) uint8 {
	if b == 0 {
		panic("solrt: modulo by zero")
	}
	return a % b
}

func SafeAddU16(a, b uint16) uint16 {
	c := a + b
	if c < a {
		panic("solrt: uint16 addition overflow")
	}
	return c
}

func SafeSubU16(a, b uint16) uint16 {
	if b > a {
		panic("solrt: uint16 subtraction underflow")
	}
	return a - b
}

func SafeMulU16(a, b uint16) uint16 {
	c := uint32(a) * uint32(b)
	if c > 0xffff {
		panic("solrt: uint16 multiplication overflow")
	}
	return uint16(c)
}

func SafeDivU16(a, b uint16) uint16 {
	if b == 0 {
		panic("solrt: division by zero")
	}
	return a / b
}

func SafeModU16(a, b uint16) uint16 {
	if b == 0 {
		panic("solrt: modulo by zero")
	}
	return a % b
}

func SafeAddU32(a, b uint32) uint32 {
	c := a + b
	if c < a {
		panic("solrt: uint32 addition overflow")
	}
	return c
}

func SafeSubU32(a, b uint32) uint32 {
	if b > a {
		panic("solrt: uint32 subtraction underflow")
	}
	return a - b
}

func SafeMulU32(a, b uint32) uint32 {
	c := uint64(a) * uint64(b)
	if c > 0xffffffff {
		panic("solrt: uint32 multiplication overflow")
	}
	return uint32(c)
}

func SafeDivU32(a, b uint32) uint32 {
	if b == 0 {
		panic("solrt: division by zero")
	}
	return a / b
}

func SafeModU32(a, b uint32) uint32 {
	if b == 0 {
		panic("solrt: modulo by zero")
	}
	return a % b
}

// SafeAddU64 suma dos uint64 haciendo panic en overflow (semántica Solidity >=0.8).
func SafeAddU64(a, b uint64) uint64 {
	c := a + b
	if c < a {
		panic("solrt: uint64 addition overflow")
	}
	return c
}

// SafeSubU64 resta dos uint64 haciendo panic en underflow (semántica Solidity >=0.8).
func SafeSubU64(a, b uint64) uint64 {
	if b > a {
		panic("solrt: uint64 subtraction underflow")
	}
	return a - b
}

// SafeMulU64 multiplica dos uint64 haciendo panic en overflow (semántica Solidity >=0.8).
func SafeMulU64(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	c := a * b
	if c/a != b {
		panic("solrt: uint64 multiplication overflow")
	}
	return c
}

// SafeDivU64 divide dos uint64 haciendo panic ante división por cero.
func SafeDivU64(a, b uint64) uint64 {
	if b == 0 {
		panic("solrt: division by zero")
	}
	return a / b
}

// SafeModU64 calcula el resto haciendo panic ante módulo por cero.
func SafeModU64(a, b uint64) uint64 {
	if b == 0 {
		panic("solrt: modulo by zero")
	}
	return a % b
}

// CheckUnsignedBitsU64 verifica que v cabe en 'bits' bits sin signo (para uint24, uint40, uint48, uint56).
func CheckUnsignedBitsU64(v uint64, bits uint) uint64 {
	if bits > 0 && bits < 64 && v >= (uint64(1)<<bits) {
		panic("solrt: uintN overflow")
	}
	return v
}

// FormatU64 formatea un uint64 para eventos chain.Emit o claves deterministas.
func FormatU64(v uint64) string {
	return strconv.FormatUint(v, 10)
}

// FormatI64 formatea un int64 para eventos chain.Emit o claves deterministas.
func FormatI64(v int64) string {
	return strconv.FormatInt(v, 10)
}

// FormatBool formatea un bool para eventos chain.Emit.
func FormatBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// KeyJoin combina múltiples componentes de clave de forma libre de colisiones
// (prefijando longitud) para mappings anidados en Solidity.
func KeyJoin(parts ...string) string {
	out := ""
	for i := 0; i < len(parts); i++ {
		if i > 0 {
			out += "|"
		}
		out += strconv.Itoa(len(parts[i])) + ":" + parts[i]
	}
	return out
}
