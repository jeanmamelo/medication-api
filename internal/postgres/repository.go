// Package postgres provides PostgreSQL adapters for the application.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jeanmamelo/medication-api/internal/medication"
)

type Repository struct {
	pool pool
}

type pool interface {
	Ping(context.Context) error
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (repository *Repository) Ready(ctx context.Context) error {
	return repository.pool.Ping(ctx)
}

func (repository *Repository) Create(ctx context.Context, item medication.Medication) error {
	_, err := repository.pool.Exec(ctx, `INSERT INTO medications (id, name, dosage, form) VALUES ($1, $2, $3, $4)`, item.ID, item.Name, item.Dosage, item.Form)
	return err
}

func (repository *Repository) Get(ctx context.Context, id string) (medication.Medication, error) {
	item, err := scanMedication(repository.pool.QueryRow(ctx, `SELECT id, name, dosage, form FROM medications WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return medication.Medication{}, medication.ErrNotFound
	}
	return item, err
}

func (repository *Repository) List(ctx context.Context, limit, offset int) ([]medication.Medication, error) {
	rows, err := repository.pool.Query(ctx, `SELECT id, name, dosage, form FROM medications ORDER BY name, id LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]medication.Medication, 0)
	for rows.Next() {
		item, err := scanMedication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Update merges the supplied fields in a single statement. A nil pointer binds as SQL
// NULL, so COALESCE keeps the stored value and concurrent partial updates cannot
// overwrite each other's fields.
func (repository *Repository) Update(ctx context.Context, id string, input medication.UpdateInput) (medication.Medication, error) {
	item, err := scanMedication(repository.pool.QueryRow(ctx, `UPDATE medications SET name = COALESCE($2, name), dosage = COALESCE($3, dosage), form = COALESCE($4, form), updated_at = NOW() WHERE id = $1 RETURNING id, name, dosage, form`, id, input.Name, input.Dosage, input.Form))
	if errors.Is(err, pgx.ErrNoRows) {
		return medication.Medication{}, medication.ErrNotFound
	}
	return item, err
}

func (repository *Repository) Delete(ctx context.Context, id string) error {
	command, err := repository.pool.Exec(ctx, `DELETE FROM medications WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return medication.ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanMedication(row rowScanner) (medication.Medication, error) {
	var item medication.Medication
	if err := row.Scan(&item.ID, &item.Name, &item.Dosage, &item.Form); err != nil {
		return medication.Medication{}, fmt.Errorf("scan medication: %w", err)
	}
	return item, nil
}
