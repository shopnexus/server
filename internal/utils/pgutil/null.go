package pgutil

import (
	"github.com/google/uuid"
	"github.com/guregu/null/v6"
	"github.com/jackc/pgx/v5/pgtype"
)

// Convert guregu/null types to pgx/pgtype types

func NullStringToPgText(s null.String) pgtype.Text {
	return pgtype.Text{String: s.String, Valid: s.Valid}
}

func NullInt64ToPgInt8(n null.Int64) pgtype.Int8 {
	return pgtype.Int8{Int64: n.Int64, Valid: n.Valid}
}

func NullInt32ToPgInt4(n null.Int32) pgtype.Int4 {
	return pgtype.Int4{Int32: int32(n.Int32), Valid: n.Valid}
}

func NullBoolToPgBool(b null.Bool) pgtype.Bool {
	return pgtype.Bool{Bool: b.Bool, Valid: b.Valid}
}

func NullFloat64ToPgFloat8(f null.Float) pgtype.Float8 {
	return pgtype.Float8{Float64: f.Float64, Valid: f.Valid}
}

func NullTimeToPgTimestamptz(t null.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.Time, Valid: t.Valid}
}

func NullUUIDToPgUUID(s uuid.NullUUID) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(s.UUID), Valid: s.Valid}
}

// Convert pgx/pgtype types to guregu/null types

func PgTextToNullString(t pgtype.Text) null.String {
	return null.NewString(t.String, t.Valid)
}

func PgInt8ToNullInt64(i pgtype.Int8) null.Int64 {
	return null.NewInt(i.Int64, i.Valid)
}

func PgInt4ToNullInt32(i pgtype.Int4) null.Int32 {
	return null.NewInt32(int32(i.Int32), i.Valid)
}

func PgBoolToNullBool(b pgtype.Bool) null.Bool {
	return null.NewBool(b.Bool, b.Valid)
}

func PgFloat8ToNullFloat(f pgtype.Float8) null.Float {
	return null.NewFloat(f.Float64, f.Valid)
}

func PgTimestamptzToNullTime(t pgtype.Timestamptz) null.Time {
	return null.NewTime(t.Time, t.Valid)
}

func PgUUIDToNullUUID(u pgtype.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: uuid.UUID(u.Bytes), Valid: u.Valid}
}
