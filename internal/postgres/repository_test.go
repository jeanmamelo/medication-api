package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jeanmamelo/medication-api/internal/medication"
)

func TestNewRepository(t *testing.T) {
	if repository := NewRepository(nil); repository == nil {
		t.Fatal("NewRepository() = nil")
	}
}

func TestRepositoryReady(t *testing.T) {
	wantErr := errors.New("database unavailable")
	repository := &Repository{pool: &fakePool{pingErr: wantErr}}

	err := repository.Ready(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Ready() error = %v, want %v", err, wantErr)
	}
}

func TestRepositoryCreate(t *testing.T) {
	item := medication.Medication{ID: "id", Name: "Paracetamol", Dosage: "500 mg", Form: "tablet"}
	pool := &fakePool{}
	repository := &Repository{pool: pool}

	if err := repository.Create(context.Background(), item); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if pool.execSQL != "INSERT INTO medications (id, name, dosage, form) VALUES ($1, $2, $3, $4)" {
		t.Errorf("Create() SQL = %q", pool.execSQL)
	}
	if want := []any{"id", "Paracetamol", "500 mg", "tablet"}; !reflect.DeepEqual(pool.execArgs, want) {
		t.Errorf("Create() args = %#v, want %#v", pool.execArgs, want)
	}

	wantErr := errors.New("insert failed")
	pool.execErr = wantErr
	if err := repository.Create(context.Background(), item); !errors.Is(err, wantErr) {
		t.Fatalf("Create() error = %v, want %v", err, wantErr)
	}
}

func TestRepositoryGet(t *testing.T) {
	pool := &fakePool{row: fakeRow{item: medication.Medication{ID: "id", Name: "Paracetamol", Dosage: "500 mg", Form: "tablet"}}}
	repository := &Repository{pool: pool}

	got, err := repository.Get(context.Background(), "id")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if want := (medication.Medication{ID: "id", Name: "Paracetamol", Dosage: "500 mg", Form: "tablet"}); got != want {
		t.Errorf("Get() = %#v, want %#v", got, want)
	}
	if pool.queryRowSQL != "SELECT id, name, dosage, form FROM medications WHERE id = $1" || !reflect.DeepEqual(pool.queryRowArgs, []any{"id"}) {
		t.Errorf("Get() query = %q, args = %#v", pool.queryRowSQL, pool.queryRowArgs)
	}
}

func TestRepositoryGetMapsNoRowsAndWrapsScanError(t *testing.T) {
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repository := &Repository{pool: pool}

	if _, err := repository.Get(context.Background(), "missing"); !errors.Is(err, medication.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}

	wantErr := errors.New("bad value")
	pool.row = fakeRow{err: wantErr}
	if _, err := repository.Get(context.Background(), "id"); !errors.Is(err, wantErr) {
		t.Fatalf("Get() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestRepositoryList(t *testing.T) {
	rows := &fakeRows{items: []medication.Medication{
		{ID: "a", Name: "Aspirin", Dosage: "100 mg", Form: "tablet"},
		{ID: "p", Name: "Paracetamol", Dosage: "500 mg", Form: "tablet"},
	}}
	pool := &fakePool{rows: rows}
	repository := &Repository{pool: pool}

	got, err := repository.List(context.Background(), 20, 4)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if !reflect.DeepEqual(got, rows.items) {
		t.Errorf("List() = %#v, want %#v", got, rows.items)
	}
	if !rows.closed {
		t.Error("List() did not close rows")
	}
	if pool.querySQL != "SELECT id, name, dosage, form FROM medications ORDER BY name, id LIMIT $1 OFFSET $2" || !reflect.DeepEqual(pool.queryArgs, []any{20, 4}) {
		t.Errorf("List() query = %q, args = %#v", pool.querySQL, pool.queryArgs)
	}
}

func TestRepositoryListReturnsQueryScanAndRowsErrors(t *testing.T) {
	queryErr := errors.New("query failed")
	repository := &Repository{pool: &fakePool{queryErr: queryErr}}
	if _, err := repository.List(context.Background(), 1, 0); !errors.Is(err, queryErr) {
		t.Fatalf("List() error = %v, want %v", err, queryErr)
	}

	scanErr := errors.New("scan failed")
	repository.pool = &fakePool{rows: &fakeRows{items: []medication.Medication{{ID: "id"}}, scanErr: scanErr}}
	if _, err := repository.List(context.Background(), 1, 0); !errors.Is(err, scanErr) {
		t.Fatalf("List() error = %v, want wrapped %v", err, scanErr)
	}

	rowsErr := errors.New("rows failed")
	repository.pool = &fakePool{rows: &fakeRows{err: rowsErr}}
	if _, err := repository.List(context.Background(), 1, 0); !errors.Is(err, rowsErr) {
		t.Fatalf("List() error = %v, want %v", err, rowsErr)
	}
}

func TestRepositoryUpdateMergesSuppliedFields(t *testing.T) {
	merged := medication.Medication{ID: "id", Name: "Paracetamol", Dosage: "750 mg", Form: "tablet"}
	pool := &fakePool{row: fakeRow{item: merged}}
	repository := &Repository{pool: pool}
	dosage := "750 mg"

	got, err := repository.Update(context.Background(), "id", medication.UpdateInput{Dosage: &dosage})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got != merged {
		t.Errorf("Update() = %#v, want %#v", got, merged)
	}
	if want := "UPDATE medications SET name = COALESCE($2, name), dosage = COALESCE($3, dosage), form = COALESCE($4, form), updated_at = NOW() WHERE id = $1 RETURNING id, name, dosage, form"; pool.queryRowSQL != want {
		t.Errorf("Update() query = %q, want %q", pool.queryRowSQL, want)
	}
	// Absent fields must reach Postgres as NULL so COALESCE keeps the stored value.
	if want := []any{"id", (*string)(nil), &dosage, (*string)(nil)}; !reflect.DeepEqual(pool.queryRowArgs, want) {
		t.Errorf("Update() args = %#v, want %#v", pool.queryRowArgs, want)
	}
}

func TestRepositoryUpdateMapsNoRows(t *testing.T) {
	repository := &Repository{pool: &fakePool{row: fakeRow{err: pgx.ErrNoRows}}}
	if _, err := repository.Update(context.Background(), "missing", medication.UpdateInput{}); !errors.Is(err, medication.ErrNotFound) {
		t.Fatalf("Update() error = %v, want ErrNotFound", err)
	}

	wantErr := errors.New("execution failed")
	repository.pool = &fakePool{row: fakeRow{err: wantErr}}
	if _, err := repository.Update(context.Background(), "id", medication.UpdateInput{}); !errors.Is(err, wantErr) {
		t.Fatalf("Update() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestRepositoryDelete(t *testing.T) {
	pool := &fakePool{commandTag: pgconn.NewCommandTag("DELETE 1")}
	repository := &Repository{pool: pool}

	if err := repository.Delete(context.Background(), "id"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if pool.execSQL != "DELETE FROM medications WHERE id = $1" || !reflect.DeepEqual(pool.execArgs, []any{"id"}) {
		t.Errorf("Delete() query = %q, args = %#v", pool.execSQL, pool.execArgs)
	}
}

func TestRepositoryDeleteErrors(t *testing.T) {
	repository := &Repository{pool: &fakePool{commandTag: pgconn.NewCommandTag("DELETE 0")}}
	if err := repository.Delete(context.Background(), "missing"); !errors.Is(err, medication.ErrNotFound) {
		t.Fatalf("Delete() error = %v, want ErrNotFound", err)
	}

	wantErr := errors.New("execution failed")
	repository.pool = &fakePool{execErr: wantErr}
	if err := repository.Delete(context.Background(), "id"); !errors.Is(err, wantErr) {
		t.Fatalf("Delete() error = %v, want %v", err, wantErr)
	}
}

type fakePool struct {
	pingErr, execErr, queryErr        error
	commandTag                        pgconn.CommandTag
	row                               pgx.Row
	rows                              pgx.Rows
	execSQL, queryRowSQL, querySQL    string
	execArgs, queryRowArgs, queryArgs []any
}

func (pool *fakePool) Ping(context.Context) error { return pool.pingErr }
func (pool *fakePool) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	pool.execSQL, pool.execArgs = sql, args
	return pool.commandTag, pool.execErr
}
func (pool *fakePool) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	pool.queryRowSQL, pool.queryRowArgs = sql, args
	return pool.row
}
func (pool *fakePool) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	pool.querySQL, pool.queryArgs = sql, args
	return pool.rows, pool.queryErr
}

type fakeRow struct {
	item medication.Medication
	err  error
}

func (row fakeRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return scanInto(dest, row.item)
}

type fakeRows struct {
	items        []medication.Medication
	index        int
	err, scanErr error
	closed       bool
}

func (rows *fakeRows) Close()                                       { rows.closed = true }
func (rows *fakeRows) Err() error                                   { return rows.err }
func (rows *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *fakeRows) Next() bool {
	if rows.index >= len(rows.items) {
		rows.closed = true
		return false
	}
	rows.index++
	return true
}
func (rows *fakeRows) Scan(dest ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	return scanInto(dest, rows.items[rows.index-1])
}
func (rows *fakeRows) Values() ([]any, error) { return nil, nil }
func (rows *fakeRows) RawValues() [][]byte    { return nil }
func (rows *fakeRows) Conn() *pgx.Conn        { return nil }

func scanInto(dest []any, item medication.Medication) error {
	values := []string{item.ID, item.Name, item.Dosage, item.Form}
	for index, value := range values {
		pointer, ok := dest[index].(*string)
		if !ok {
			return errors.New("destination is not a string pointer")
		}
		*pointer = value
	}
	return nil
}
