package antigravity

import (
	"encoding/binary"
	"math"
)

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

// forEachField walks one protobuf message without interpreting it. It stops when fn returns false
// or the wire data is malformed.
func forEachField(data []byte, fn func(num, wire int, value uint64, raw []byte) bool) bool {
	for i := 0; i < len(data); {
		key, next, ok := readVarint(data, i)
		if !ok || key>>3 == 0 || key>>3 > math.MaxInt32 {
			return false
		}
		num, wire := int(key>>3), int(key&7)
		i = next
		switch wire {
		case wireVarint:
			v, next, ok := readVarint(data, i)
			if !ok {
				return false
			}
			i = next
			if !fn(num, wire, v, nil) {
				return false
			}
		case wireFixed64:
			if len(data)-i < 8 {
				return false
			}
			if !fn(num, wire, binary.LittleEndian.Uint64(data[i:]), nil) {
				return false
			}
			i += 8
		case wireBytes:
			n, next, ok := readVarint(data, i)
			if !ok || n > uint64(len(data)-next) {
				return false
			}
			i = next
			raw := data[i : i+int(n)]
			i += int(n)
			if !fn(num, wire, 0, raw) {
				return false
			}
		case wireFixed32:
			if len(data)-i < 4 {
				return false
			}
			if !fn(num, wire, uint64(binary.LittleEndian.Uint32(data[i:])), nil) {
				return false
			}
			i += 4
		default:
			return false
		}
	}
	return true
}

func readVarint(data []byte, i int) (uint64, int, bool) {
	var v uint64
	for shift := uint(0); shift < 64; shift += 7 {
		if i >= len(data) {
			return 0, i, false
		}
		b := data[i]
		i++
		if shift == 63 && b > 1 {
			return 0, i, false
		}
		v |= uint64(b&0x7f) << shift
		if b < 0x80 {
			return v, i, true
		}
	}
	return 0, i, false
}

func bytesField(data []byte, num int) ([]byte, bool) {
	var out []byte
	found := false
	forEachField(data, func(n, wire int, _ uint64, raw []byte) bool {
		if n == num && wire == wireBytes {
			out, found = raw, true
			return false
		}
		return true
	})
	return out, found
}

func varintField(data []byte, num int) (uint64, bool) {
	var out uint64
	found := false
	forEachField(data, func(n, wire int, value uint64, _ []byte) bool {
		if n == num && wire == wireVarint {
			out, found = value, true
			return false
		}
		return true
	})
	return out, found
}
