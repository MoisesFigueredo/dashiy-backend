package database

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func UUID(uuidValue uuid.UUID) pgtype.UUID {
	return pgtype.UUID{
		Bytes: uuidValue,
		Valid: true,
	}
}

func UUIDPointer(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}

	return UUID(*value)
}

func UUIDFromNullable(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}

	id := uuid.UUID(value.Bytes)
	return &id
}

func Timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  value.UTC(),
		Valid: true,
	}
}
