package solrt

// --- Aritmética signed checked exacta por ancho (int8..int64, Solidity >=0.8) ---

const (
	minInt8  int8  = -128
	maxInt8  int8  = 127
	minInt16 int16 = -32768
	maxInt16 int16 = 32767
	minInt32 int32 = -2147483648
	maxInt32 int32 = 2147483647
	minInt64 int64 = -9223372036854775808
	maxInt64 int64 = 9223372036854775807
)

func SafeAddI8(a, b int8) int8 {
	s := int16(a) + int16(b)
	if s < int16(minInt8) || s > int16(maxInt8) {
		panic("solrt: int8 addition overflow")
	}
	return int8(s)
}

func SafeSubI8(a, b int8) int8 {
	d := int16(a) - int16(b)
	if d < int16(minInt8) || d > int16(maxInt8) {
		panic("solrt: int8 subtraction overflow")
	}
	return int8(d)
}

func SafeMulI8(a, b int8) int8 {
	p := int16(a) * int16(b)
	if p < int16(minInt8) || p > int16(maxInt8) {
		panic("solrt: int8 multiplication overflow")
	}
	return int8(p)
}

func SafeDivI8(a, b int8) int8 {
	if b == 0 {
		panic("solrt: division by zero")
	}
	if a == minInt8 && b == -1 {
		panic("solrt: int8 division overflow")
	}
	return a / b
}

func SafeModI8(a, b int8) int8 {
	if b == 0 {
		panic("solrt: modulo by zero")
	}
	return a % b
}

func SafeNegI8(a int8) int8 {
	if a == minInt8 {
		panic("solrt: int8 negation overflow")
	}
	return -a
}

func SafeAddI16(a, b int16) int16 {
	s := int32(a) + int32(b)
	if s < int32(minInt16) || s > int32(maxInt16) {
		panic("solrt: int16 addition overflow")
	}
	return int16(s)
}

func SafeSubI16(a, b int16) int16 {
	d := int32(a) - int32(b)
	if d < int32(minInt16) || d > int32(maxInt16) {
		panic("solrt: int16 subtraction overflow")
	}
	return int16(d)
}

func SafeMulI16(a, b int16) int16 {
	p := int32(a) * int32(b)
	if p < int32(minInt16) || p > int32(maxInt16) {
		panic("solrt: int16 multiplication overflow")
	}
	return int16(p)
}

func SafeDivI16(a, b int16) int16 {
	if b == 0 {
		panic("solrt: division by zero")
	}
	if a == minInt16 && b == -1 {
		panic("solrt: int16 division overflow")
	}
	return a / b
}

func SafeModI16(a, b int16) int16 {
	if b == 0 {
		panic("solrt: modulo by zero")
	}
	return a % b
}

func SafeNegI16(a int16) int16 {
	if a == minInt16 {
		panic("solrt: int16 negation overflow")
	}
	return -a
}

func SafeAddI32(a, b int32) int32 {
	s := int64(a) + int64(b)
	if s < int64(minInt32) || s > int64(maxInt32) {
		panic("solrt: int32 addition overflow")
	}
	return int32(s)
}

func SafeSubI32(a, b int32) int32 {
	d := int64(a) - int64(b)
	if d < int64(minInt32) || d > int64(maxInt32) {
		panic("solrt: int32 subtraction overflow")
	}
	return int32(d)
}

func SafeMulI32(a, b int32) int32 {
	p := int64(a) * int64(b)
	if p < int64(minInt32) || p > int64(maxInt32) {
		panic("solrt: int32 multiplication overflow")
	}
	return int32(p)
}

func SafeDivI32(a, b int32) int32 {
	if b == 0 {
		panic("solrt: division by zero")
	}
	if a == minInt32 && b == -1 {
		panic("solrt: int32 division overflow")
	}
	return a / b
}

func SafeModI32(a, b int32) int32 {
	if b == 0 {
		panic("solrt: modulo by zero")
	}
	return a % b
}

func SafeNegI32(a int32) int32 {
	if a == minInt32 {
		panic("solrt: int32 negation overflow")
	}
	return -a
}

func SafeAddI64(a, b int64) int64 {
	if (b > 0 && a > maxInt64-b) || (b < 0 && a < minInt64-b) {
		panic("solrt: int64 addition overflow")
	}
	return a + b
}

func SafeSubI64(a, b int64) int64 {
	if (b < 0 && a > maxInt64+b) || (b > 0 && a < minInt64+b) {
		panic("solrt: int64 subtraction overflow")
	}
	return a - b
}

func SafeMulI64(a, b int64) int64 {
	if a == 0 || b == 0 {
		return 0
	}
	if (a == minInt64 && b == -1) || (b == minInt64 && a == -1) {
		panic("solrt: int64 multiplication overflow")
	}
	c := a * b
	if c/a != b {
		panic("solrt: int64 multiplication overflow")
	}
	return c
}

func SafeDivI64(a, b int64) int64 {
	if b == 0 {
		panic("solrt: division by zero")
	}
	if a == minInt64 && b == -1 {
		panic("solrt: int64 division overflow")
	}
	return a / b
}

func SafeModI64(a, b int64) int64 {
	if b == 0 {
		panic("solrt: modulo by zero")
	}
	return a % b
}

func SafeNegI64(a int64) int64 {
	if a == minInt64 {
		panic("solrt: int64 negation overflow")
	}
	return -a
}

// CheckSignedBitsI64 verifica que v cabe en 'bits' bits con signo (int24, int40, int48, int56).
func CheckSignedBitsI64(v int64, bits uint) int64 {
	if bits > 0 && bits < 64 {
		minV := -(int64(1) << (bits - 1))
		maxV := (int64(1) << (bits - 1)) - 1
		if v < minV || v > maxV {
			panic("solrt: intN overflow")
		}
	}
	return v
}
