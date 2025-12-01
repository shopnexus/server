package sharedmodel

import (
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

func (c Concurrency) Div(divisor int64) Concurrency {
	return c / Concurrency(divisor)
}

func (c Concurrency) String() string {
	return strconv.FormatFloat(float64(c)/FloatingPointPrecision, 'f', -1, 64)
}

func (c Concurrency) MarshalJSON() ([]byte, error) {
	return []byte(c.String()), nil
}

// Float64 returns the Concurrency value as a float64 but scaled by FloatingPointPrecision.
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
	return []byte(nc.Concurrency.String()), nil
}

func (nc *NullConcurrency) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		nc.Valid = false
		return nil
	}
	value, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return err
	}
	nc.Concurrency = FloatToConcurrency(value)
	nc.Valid = true
	return nil
}

func (nc NullConcurrency) ToNullInt64() null.Int {
	if !nc.Valid {
		return null.Int{}
	}
	return null.IntFrom(int64(nc.Concurrency))
}

func NullConcurrencyFromNullInt64(nInt null.Int) NullConcurrency {
	if !nInt.Valid {
		return NullConcurrency{Valid: false}
	}
	return NullConcurrency{
		Concurrency: Concurrency(nInt.Int64 * FloatingPointPrecision),
		Valid:       true,
	}
}
