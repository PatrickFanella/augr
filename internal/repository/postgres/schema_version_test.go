package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type fakeSchemaVersionRow struct {
	version int
	err     error
}

func (r fakeSchemaVersionRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("unexpected scan arity")
	}
	ptr, ok := dest[0].(*int)
	if !ok {
		return errors.New("unexpected scan target")
	}
	*ptr = r.version
	return nil
}

type fakeSchemaVersionQuerier struct {
	row schemaVersionRow
}

func (q fakeSchemaVersionQuerier) QueryRow(context.Context, string, ...any) schemaVersionRow {
	return q.row
}

func TestCompareSchemaVersion(t *testing.T) {
	tests := []struct {
		name     string
		current  int
		required int
		want     schemaVersionState
	}{
		{name: "behind", current: 28, required: RequiredSchemaVersion, want: schemaVersionBehind},
		{name: "match", current: RequiredSchemaVersion, required: RequiredSchemaVersion, want: schemaVersionMatch},
		{name: "ahead", current: RequiredSchemaVersion + 1, required: RequiredSchemaVersion, want: schemaVersionAhead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareSchemaVersion(tt.current, tt.required); got != tt.want {
				t.Fatalf("CompareSchemaVersion(%d, %d) = %q, want %q", tt.current, tt.required, got, tt.want)
			}
		})
	}
}

func TestCurrentSchemaVersion(t *testing.T) {
	got, err := currentSchemaVersion(context.Background(), fakeSchemaVersionQuerier{
		row: fakeSchemaVersionRow{version: 28},
	})
	if err != nil {
		t.Fatalf("currentSchemaVersion() error = %v", err)
	}
	if got != 28 {
		t.Fatalf("currentSchemaVersion() = %d, want 28", got)
	}
}

func TestCurrentSchemaVersion_EmptyTableError(t *testing.T) {
	_, err := currentSchemaVersion(context.Background(), fakeSchemaVersionQuerier{
		row: fakeSchemaVersionRow{err: pgx.ErrNoRows},
	})
	if err == nil {
		t.Fatal("currentSchemaVersion() error = nil, want error")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("currentSchemaVersion() error = %v, want pgx.ErrNoRows", err)
	}
}

func TestCurrentSchemaVersion_ReadError(t *testing.T) {
	readErr := errors.New("boom")
	_, err := currentSchemaVersion(context.Background(), fakeSchemaVersionQuerier{
		row: fakeSchemaVersionRow{err: readErr},
	})
	if err == nil {
		t.Fatal("currentSchemaVersion() error = nil, want error")
	}
	if !errors.Is(err, readErr) {
		t.Fatalf("currentSchemaVersion() error = %v, want wrapped readErr", err)
	}
}
