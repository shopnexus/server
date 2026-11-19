package seedpack

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// Matrix is a two-dimensional float16 .npy file (format 1.0, little
// endian, C order), read and written a row at a time. A new one is all
// zeros, so a writer can fill it in any order and resume.
type Matrix struct {
	f      *os.File
	Rows   int
	Cols   int
	offset int64
}

var npyMagic = []byte("\x93NUMPY")

// CreateMatrix writes a rows × cols matrix of zeros at path, replacing any.
func CreateMatrix(path string, rows, cols int) (*Matrix, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("make %s: %w", filepath.Dir(path), err)
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	dict := fmt.Sprintf("{'descr': '<f2', 'fortran_order': False, 'shape': (%d, %d), }", rows, cols)
	// Magic, version and length take 10 bytes; the header ends in a newline
	// and pads the data to 64 bytes.
	pad := 64 - (10+len(dict)+1)%64
	if pad == 64 {
		pad = 0
	}
	header := dict + string(bytes.Repeat([]byte{' '}, pad)) + "\n"
	var b bytes.Buffer
	b.Write(npyMagic)
	b.Write([]byte{1, 0})
	_ = binary.Write(&b, binary.LittleEndian, uint16(len(header))) //nolint:gosec // a header of under 200 bytes
	b.WriteString(header)
	m := &Matrix{f: f, Rows: rows, Cols: cols, offset: int64(b.Len())}
	if _, err = f.Write(b.Bytes()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	if err = f.Truncate(m.offset + int64(rows)*int64(cols)*2); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("size %s: %w", path, err)
	}
	return m, nil
}

var npyShape = regexp.MustCompile(`'shape':\s*\((\d+),\s*(\d+)\)`)

// OpenMatrix opens a float16 matrix; writable lets SetRow write to it.
func OpenMatrix(path string, writable bool) (*Matrix, error) {
	flag := os.O_RDONLY
	if writable {
		flag = os.O_RDWR
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	m, err := readHeader(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if want := m.offset + int64(m.Rows)*int64(m.Cols)*2; info.Size() != want {
		_ = f.Close()
		return nil, fmt.Errorf("%s is %d bytes, its header says %d", path, info.Size(), want)
	}
	return m, nil
}

func readHeader(f *os.File) (*Matrix, error) {
	head := make([]byte, 10)
	if _, err := io.ReadFull(f, head); err != nil {
		return nil, fmt.Errorf("read the header: %w", err)
	}
	if !bytes.Equal(head[:6], npyMagic) || head[6] != 1 {
		return nil, errors.New("not a version 1 .npy file")
	}
	n := int(binary.LittleEndian.Uint16(head[8:]))
	dict := make([]byte, n)
	if _, err := io.ReadFull(f, dict); err != nil {
		return nil, fmt.Errorf("read the header: %w", err)
	}
	if !bytes.Contains(dict, []byte("'descr': '<f2'")) || !bytes.Contains(dict, []byte("'fortran_order': False")) {
		return nil, fmt.Errorf("want little-endian float16 in C order, the header is %s", bytes.TrimSpace(dict))
	}
	shape := npyShape.FindSubmatch(dict)
	if shape == nil {
		return nil, fmt.Errorf("want a two-dimensional shape, the header is %s", bytes.TrimSpace(dict))
	}
	rows, _ := strconv.Atoi(string(shape[1]))
	cols, _ := strconv.Atoi(string(shape[2]))
	return &Matrix{f: f, Rows: rows, Cols: cols, offset: int64(10 + n)}, nil
}

// Row reads row i, nil when it is all zeros: no vector.
func (m *Matrix) Row(i int) ([]float32, error) {
	if i < 0 || i >= m.Rows {
		return nil, fmt.Errorf("row %d of %d", i, m.Rows)
	}
	raw := make([]byte, m.Cols*2)
	if _, err := m.f.ReadAt(raw, m.offset+int64(i)*int64(m.Cols)*2); err != nil {
		return nil, fmt.Errorf("read row %d: %w", i, err)
	}
	out := make([]float32, m.Cols)
	zero := true
	for j := range out {
		h := binary.LittleEndian.Uint16(raw[j*2:])
		zero = zero && h&0x7FFF == 0
		out[j] = HalfToFloat(h)
	}
	if zero {
		return nil, nil
	}
	return out, nil
}

// SetRow writes v, of Cols values, as row i.
func (m *Matrix) SetRow(i int, v []float32) error {
	if i < 0 || i >= m.Rows || len(v) != m.Cols {
		return fmt.Errorf("row %d of %d with %d values, want %d", i, m.Rows, len(v), m.Cols)
	}
	raw := make([]byte, m.Cols*2)
	for j, x := range v {
		binary.LittleEndian.PutUint16(raw[j*2:], FloatToHalf(x))
	}
	if _, err := m.f.WriteAt(raw, m.offset+int64(i)*int64(m.Cols)*2); err != nil {
		return fmt.Errorf("write row %d: %w", i, err)
	}
	return nil
}

func (m *Matrix) Close() error {
	if err := m.f.Close(); err != nil {
		return fmt.Errorf("close the matrix: %w", err)
	}
	return nil
}

// FloatToHalf rounds f to the nearest IEEE 754 half, ties to even; past
// ±65504 it is infinity.
func FloatToHalf(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int(b>>23&0xFF) - 127 + 15
	mant := b & 0x7FFFFF
	switch {
	case b&0x7FFFFFFF > 0x7F800000: // NaN
		return sign | 0x7E00
	case exp >= 31: // too large, or infinity
		return sign | 0x7C00
	case exp <= 0: // a subnormal half, or zero
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint32(14 - exp)
		half := mant >> shift
		rest := mant & (1<<shift - 1)
		mid := uint32(1) << (shift - 1)
		if rest > mid || (rest == mid && half&1 == 1) {
			half++
		}
		return sign | uint16(half) //nolint:gosec // at most 0x400
	}
	half := uint32(exp)<<10 | mant>>13
	rest := mant & 0x1FFF
	if rest > 0x1000 || (rest == 0x1000 && half&1 == 1) {
		half++ // may carry into the exponent, up to infinity
	}
	return sign | uint16(half) //nolint:gosec // at most 0x7C00
}

// HalfToFloat widens an IEEE 754 half.
func HalfToFloat(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1F
	mant := uint32(h & 0x3FF)
	switch {
	case exp == 0 && mant == 0:
		return math.Float32frombits(sign)
	case exp == 0: // subnormal: normalize it
		e := uint32(127 - 15 + 1)
		for mant&0x400 == 0 {
			mant <<= 1
			e--
		}
		return math.Float32frombits(sign | e<<23 | (mant&0x3FF)<<13)
	case exp == 0x1F:
		return math.Float32frombits(sign | 0x7F800000 | mant<<13)
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
}
