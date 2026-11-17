package solrt

// U256 representa un entero sin signo de 256 bits en Gno puro (Go 1.17)
// con aritmética checked por defecto (semántica Solidity >=0.8).
// Almacenado en 4 palabras de 64 bits en orden little-endian (w[0] = LSB, w[3] = MSB).
type U256 struct {
	w [4]uint64
}

// U256Zero devuelve el valor cero de 256 bits.
func U256Zero() U256 {
	return U256{}
}

// U256FromU64 construye un U256 a partir de un uint64.
func U256FromU64(v uint64) U256 {
	return U256{w: [4]uint64{v, 0, 0, 0}}
}

// U256FromString parsea un entero decimal no negativo en U256 (panic si es inválido u overflow).
func U256FromString(s string) U256 {
	if s == "" {
		panic("solrt: empty U256 string")
	}
	res := U256Zero()
	ten := U256FromU64(10)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '_' {
			continue
		}
		if ch < '0' || ch > '9' {
			panic("solrt: invalid U256 digit")
		}
		res = res.Mul(ten).Add(U256FromU64(uint64(ch - '0')))
	}
	return res
}

// IsZero devuelve true si u == 0.
func (u U256) IsZero() bool {
	return (u.w[0] | u.w[1] | u.w[2] | u.w[3]) == 0
}

// Cmp compara u y v: devuelve -1 si u < v, 0 si u == v, +1 si u > v.
func (u U256) Cmp(v U256) int {
	for i := 3; i >= 0; i-- {
		if u.w[i] < v.w[i] {
			return -1
		}
		if u.w[i] > v.w[i] {
			return 1
		}
	}
	return 0
}

func (u U256) Eq(v U256) bool  { return u.Cmp(v) == 0 }
func (u U256) Neq(v U256) bool { return u.Cmp(v) != 0 }
func (u U256) Lt(v U256) bool  { return u.Cmp(v) < 0 }
func (u U256) Lte(v U256) bool { return u.Cmp(v) <= 0 }
func (u U256) Gt(v U256) bool  { return u.Cmp(v) > 0 }
func (u U256) Gte(v U256) bool { return u.Cmp(v) >= 0 }

// Uint64 convierte u a uint64 haciendo panic si excede 64 bits.
func (u U256) Uint64() uint64 {
	if (u.w[1] | u.w[2] | u.w[3]) != 0 {
		panic("solrt: U256 overflows uint64")
	}
	return u.w[0]
}

// UncheckedAdd suma con wrap-around (para bloques unchecked {}).
func (u U256) UncheckedAdd(v U256) U256 {
	res, _ := addWithCarry256(u, v)
	return res
}

// Add suma dos U256 haciendo panic en overflow.
func (u U256) Add(v U256) U256 {
	res, carry := addWithCarry256(u, v)
	if carry != 0 {
		panic("solrt: uint256 addition overflow")
	}
	return res
}

// UncheckedSub resta con wrap-around (para bloques unchecked {}).
func (u U256) UncheckedSub(v U256) U256 {
	res, _ := subWithBorrow256(u, v)
	return res
}

// Sub resta dos U256 haciendo panic en underflow.
func (u U256) Sub(v U256) U256 {
	res, borrow := subWithBorrow256(u, v)
	if borrow != 0 {
		panic("solrt: uint256 subtraction underflow")
	}
	return res
}

// Mul multiplica dos U256 haciendo panic en overflow de 256 bits.
func (u U256) Mul(v U256) U256 {
	prod := mul512(u, v)
	if (prod[4] | prod[5] | prod[6] | prod[7]) != 0 {
		panic("solrt: uint256 multiplication overflow")
	}
	return U256{w: [4]uint64{prod[0], prod[1], prod[2], prod[3]}}
}

// UncheckedMul multiplica dos U256 conservando los 256 bits bajos.
func (u U256) UncheckedMul(v U256) U256 {
	prod := mul512(u, v)
	return U256{w: [4]uint64{prod[0], prod[1], prod[2], prod[3]}}
}

// Div divide u / v haciendo panic si v == 0.
func (u U256) Div(v U256) U256 {
	q, _ := divMod256(u, v)
	return q
}

// Mod calcula u % v haciendo panic si v == 0.
func (u U256) Mod(v U256) U256 {
	_, r := divMod256(u, v)
	return r
}

// MulDiv calcula floor(x * y / denom) con precisión intermedia de 512 bits
// (equivalente a PRBMath.mulDiv / OpenZeppelin Math.mulDiv).
func MulDiv(x, y, denom U256) U256 {
	if denom.IsZero() {
		panic("solrt: MulDiv division by zero")
	}
	prod := mul512(x, y)
	q, _ := divMod512By256(prod, denom)
	if (q[4] | q[5] | q[6] | q[7]) != 0 {
		panic("solrt: MulDiv result overflows uint256")
	}
	return U256{w: [4]uint64{q[0], q[1], q[2], q[3]}}
}

// Pow calcula base^exp con overflow checked.
func (u U256) Pow(exp uint64) U256 {
	res := U256FromU64(1)
	base := u
	e := exp
	for e > 0 {
		if (e & 1) == 1 {
			res = res.Mul(base)
		}
		e >>= 1
		if e > 0 {
			base = base.Mul(base)
		}
	}
	return res
}

// String devuelve la representación decimal canónica de u.
func (u U256) String() string {
	if u.IsZero() {
		return "0"
	}
	ten := U256FromU64(10)
	cur := u
	var digits []byte
	for !cur.IsZero() {
		q, r := divMod256(cur, ten)
		digits = append(digits, byte('0'+r.w[0]))
		cur = q
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

func add64(a, b, carry uint64) (sum, carryOut uint64) {
	s1 := a + b
	c1 := uint64(0)
	if s1 < a {
		c1 = 1
	}
	s2 := s1 + carry
	c2 := uint64(0)
	if s2 < s1 {
		c2 = 1
	}
	return s2, c1 + c2
}

func sub64(a, b, borrow uint64) (diff, borrowOut uint64) {
	d1 := a - b
	b1 := uint64(0)
	if a < b {
		b1 = 1
	}
	d2 := d1 - borrow
	b2 := uint64(0)
	if d1 < borrow {
		b2 = 1
	}
	return d2, b1 + b2
}

func mul64(a, b uint64) (hi, lo uint64) {
	const mask32 = 0xffffffff
	a0 := a & mask32
	a1 := a >> 32
	b0 := b & mask32
	b1 := b >> 32

	p0 := a0 * b0
	p1 := a1 * b0
	p2 := a0 * b1
	p3 := a1 * b1

	mid := (p0 >> 32) + (p1 & mask32) + (p2 & mask32)
	lo = (p0 & mask32) | (mid << 32)
	hi = p3 + (p1 >> 32) + (p2 >> 32) + (mid >> 32)
	return hi, lo
}

func addWithCarry256(u, v U256) (U256, uint64) {
	var out U256
	var c uint64
	for i := 0; i < 4; i++ {
		out.w[i], c = add64(u.w[i], v.w[i], c)
	}
	return out, c
}

func subWithBorrow256(u, v U256) (U256, uint64) {
	var out U256
	var b uint64
	for i := 0; i < 4; i++ {
		out.w[i], b = sub64(u.w[i], v.w[i], b)
	}
	return out, b
}

func mul512(u, v U256) [8]uint64 {
	var out [8]uint64
	for i := 0; i < 4; i++ {
		var carry uint64
		for j := 0; j < 4; j++ {
			hi, lo := mul64(u.w[i], v.w[j])
			sum1, c1 := add64(out[i+j], lo, carry)
			out[i+j] = sum1
			carry = hi + c1
		}
		out[i+4] += carry
	}
	return out
}

func divMod256(u, v U256) (q, r U256) {
	if v.IsZero() {
		panic("solrt: uint256 division by zero")
	}
	if u.Lt(v) {
		return U256Zero(), u
	}
	if u.Eq(v) {
		return U256FromU64(1), U256Zero()
	}
	for bit := 255; bit >= 0; bit-- {
		r = shl1_256(r)
		if getBit256(u, bit) != 0 {
			r.w[0] |= 1
		}
		if r.Gte(v) {
			r = r.Sub(v)
			setBit256(&q, bit)
		}
	}
	return q, r
}

func divMod512By256(num [8]uint64, denom U256) (q [8]uint64, r U256) {
	if denom.IsZero() {
		panic("solrt: division by zero")
	}
	for bit := 511; bit >= 0; bit-- {
		rOverflow := (r.w[3] >> 63) != 0
		r = shl1_256(r)
		if ((num[bit/64] >> uint(bit%64)) & 1) != 0 {
			r.w[0] |= 1
		}
		if rOverflow || r.Gte(denom) {
			r, _ = subWithBorrow256(r, denom)
			q[bit/64] |= uint64(1) << uint(bit%64)
		}
	}
	return q, r
}

func shl1_256(u U256) U256 {
	return U256{w: [4]uint64{
		u.w[0] << 1,
		(u.w[1] << 1) | (u.w[0] >> 63),
		(u.w[2] << 1) | (u.w[1] >> 63),
		(u.w[3] << 1) | (u.w[2] >> 63),
	}}
}

func getBit256(u U256, bit int) uint64 {
	return (u.w[bit/64] >> uint(bit%64)) & 1
}

func setBit256(u *U256, bit int) {
	u.w[bit/64] |= uint64(1) << uint(bit%64)
}
