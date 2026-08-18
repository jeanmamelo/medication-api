// Package medication contains the application's medication domain and use cases.
package medication

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotFound          = errors.New("medication not found")
	ErrConflict          = errors.New("medication already exists")
	ErrInvalidPagination = errors.New("invalid pagination")
	ErrValidation        = errors.New("validation failed")
)

const (
	// DefaultPageSize and MaxPageSize bound collection reads. They are exported so the
	// transport layer applies the same limits instead of duplicating them.
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// ValidationError identifies the field that failed validation. Its message carries no
// internal detail and is safe to return to clients.
type ValidationError struct {
	Field string
}

func (err *ValidationError) Error() string {
	return err.Field + " is required"
}

func (err *ValidationError) Unwrap() error {
	return ErrValidation
}

// ValidatePagination is the single definition of the pagination bounds.
func ValidatePagination(limit, offset int) error {
	if limit < 1 || limit > MaxPageSize || offset < 0 {
		return ErrInvalidPagination
	}
	return nil
}

type Medication struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Dosage string `json:"dosage"`
	Form   string `json:"form"`
}

type CreateInput struct {
	Name   string
	Dosage string
	Form   string
}

// UpdateInput uses pointers so an absent property differs from an empty value.
type UpdateInput struct {
	Name   *string
	Dosage *string
	Form   *string
}

type Repository interface {
	Create(context.Context, Medication) error
	Get(context.Context, string) (Medication, error)
	List(context.Context, int, int) ([]Medication, error)
	// Update merges the supplied fields into the stored row and returns the result.
	// Merging in the repository keeps a partial update atomic.
	Update(context.Context, string, UpdateInput) (Medication, error)
	Delete(context.Context, string) error
}

type IDGenerator interface {
	New() (string, error)
}

type Service struct {
	repository  Repository
	idGenerator IDGenerator
}

func NewService(repository Repository, idGenerator IDGenerator) *Service {
	return &Service{repository: repository, idGenerator: idGenerator}
}

func (service *Service) Create(ctx context.Context, input CreateInput) (Medication, error) {
	medication, err := medicationFromInput(input)
	if err != nil {
		return Medication{}, err
	}

	medication.ID, err = service.idGenerator.New()
	if err != nil {
		return Medication{}, fmt.Errorf("generate medication ID: %w", err)
	}
	if err := service.repository.Create(ctx, medication); err != nil {
		return Medication{}, fmt.Errorf("create medication: %w", err)
	}

	return medication, nil
}

func (service *Service) Get(ctx context.Context, id string) (Medication, error) {
	return service.repository.Get(ctx, id)
}

func (service *Service) List(ctx context.Context, limit, offset int) ([]Medication, error) {
	if err := ValidatePagination(limit, offset); err != nil {
		return nil, err
	}
	return service.repository.List(ctx, limit, offset)
}

// Update validates only the supplied fields; absent ones keep values that were already
// validated when they were written. The merge happens in the repository so concurrent
// partial updates cannot overwrite each other.
func (service *Service) Update(ctx context.Context, id string, input UpdateInput) (Medication, error) {
	normalized, err := normalizeUpdate(input)
	if err != nil {
		return Medication{}, err
	}

	return service.repository.Update(ctx, id, normalized)
}

func (service *Service) Delete(ctx context.Context, id string) error {
	return service.repository.Delete(ctx, id)
}

func medicationFromInput(input CreateInput) (Medication, error) {
	medication := Medication{Name: input.Name, Dosage: input.Dosage, Form: input.Form}
	if err := validate(&medication); err != nil {
		return Medication{}, err
	}
	return medication, nil
}

func validate(medication *Medication) error {
	medication.Name = strings.TrimSpace(medication.Name)
	medication.Dosage = strings.TrimSpace(medication.Dosage)
	medication.Form = strings.TrimSpace(medication.Form)

	if err := validateField("name", medication.Name); err != nil {
		return err
	}
	if err := validateField("dosage", medication.Dosage); err != nil {
		return err
	}
	return validateField("form", medication.Form)
}

// normalizeUpdate trims each supplied field and rejects any that becomes empty.
func normalizeUpdate(input UpdateInput) (UpdateInput, error) {
	normalized := UpdateInput{}
	for _, field := range []struct {
		name   string
		value  *string
		target **string
	}{
		{name: "name", value: input.Name, target: &normalized.Name},
		{name: "dosage", value: input.Dosage, target: &normalized.Dosage},
		{name: "form", value: input.Form, target: &normalized.Form},
	} {
		if field.value == nil {
			continue
		}
		trimmed := strings.TrimSpace(*field.value)
		if err := validateField(field.name, trimmed); err != nil {
			return UpdateInput{}, err
		}
		*field.target = &trimmed
	}

	return normalized, nil
}

func validateField(name, value string) error {
	if value == "" {
		return &ValidationError{Field: name}
	}
	return nil
}

// RandomUUIDGenerator produces RFC 4122 version 4 UUIDs using crypto/rand.
type RandomUUIDGenerator struct{}

func (RandomUUIDGenerator) New() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16]), nil
}
