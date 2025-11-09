package pgutil

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func StringToPgText(strings string) pgtype.Text {
	return pgtype.Text{String: strings, Valid: true}
}

func Int64ToPgInt8(n int64) pgtype.Int8 {
	return pgtype.Int8{Int64: n, Valid: true}
}

func Int32ToPgInt4(n int32) pgtype.Int4 {
	return pgtype.Int4{Int32: n, Valid: true}
}

func BoolToPgBool(b bool) pgtype.Bool {
	return pgtype.Bool{Bool: b, Valid: true}
}

func Float32ToPgFloat4(f float32) pgtype.Float4 {
	return pgtype.Float4{Float32: f, Valid: true}
}

func Float64ToPgFloat8(f float64) pgtype.Float8 {
	return pgtype.Float8{Float64: f, Valid: true}
}

func TimeToPgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func UUIDToPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(id), Valid: true}
}
