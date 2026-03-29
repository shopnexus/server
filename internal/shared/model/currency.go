package sharedmodel

import (
	"database/sql/driver"
	"strconv"

	"github.com/guregu/null/v6"
)

const FloatingPointPrecision = 1e9

type Concurrency int64

func (c Concurrency) Add(other Concurrency) Concurrency {
	return c + other
}

func (c Concurrency) Sub(other Concurrency) Concurrency {
	return c - other
}

func (c Concurrency) Mul(factor int64) Concurrency {
	return c * Concurrency(factor)
}

func (c Concurrency) String() string {
	return strconv.FormatFloat(float64(c)/FloatingPointPrecision, 'f', -1, 64)
}

func (c Concurrency) MarshalJSON() ([]byte, error) {
	buf := strconv.AppendFloat(nil, float64(c)/FloatingPointPrecision, 'f', -1, 64)
	return buf, nil
}

func (c *Concurrency) UnmarshalJSON(data []byte) error {
	value, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return err
	}
	*c = FloatToConcurrency(value)
	return nil
}

func (c Concurrency) Float64() float64 {
	return float64(c) / FloatingPointPrecision
}

func Int64ToConcurrency(v int64) Concurrency {
	return Concurrency(v * FloatingPointPrecision)
}

func FloatToConcurrency(v float64) Concurrency {
	return Concurrency(v * FloatingPointPrecision)
}

type NullConcurrency struct {
	Concurrency Concurrency
	Valid       bool
}

func (nc NullConcurrency) MarshalJSON() ([]byte, error) {
	if !nc.Valid {
		return []byte("null"), nil
	}
	return nc.Concurrency.MarshalJSON()
}

func (nc *NullConcurrency) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		nc.Valid = false
		return nil
	}
	if err := nc.Concurrency.UnmarshalJSON(data); err != nil {
		return err
	}
	nc.Valid = true
	return nil
}

// Value implements driver.Valuer so the validator's ParseNullable recognizes NullConcurrency.
func (nc NullConcurrency) Value() (driver.Value, error) {
	if !nc.Valid {
		return nil, nil
	}
	return int64(nc.Concurrency), nil
}

func (nc NullConcurrency) ToNullInt64() null.Int {
	if !nc.Valid {
		return null.Int{}
	}
	return null.IntFrom(int64(nc.Concurrency))
}

// NullConcurrencyFromNullInt64 converts a null.Int (already in scaled representation) to NullConcurrency.
func NullConcurrencyFromNullInt64(nInt null.Int) NullConcurrency {
	if !nInt.Valid {
		return NullConcurrency{Valid: false}
	}
	return NullConcurrency{
		Concurrency: Concurrency(nInt.Int64),
		Valid:       true,
	}
}
