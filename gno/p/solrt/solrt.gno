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
