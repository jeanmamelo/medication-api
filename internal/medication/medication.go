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
	ErrInvalidPagination = errors.New("invalid pagination")
)

const (
	maxPageSize = 100
)

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
	Update(context.Context, Medication) error
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
	if limit < 1 || limit > maxPageSize || offset < 0 {
		return nil, ErrInvalidPagination
	}
	return service.repository.List(ctx, limit, offset)
}

func (service *Service) Update(ctx context.Context, id string, input UpdateInput) (Medication, error) {
	current, err := service.repository.Get(ctx, id)
	if err != nil {
		return Medication{}, err
	}
	if input.Name != nil {
		current.Name = *input.Name
	}
	if input.Dosage != nil {
		current.Dosage = *input.Dosage
	}
	if input.Form != nil {
		current.Form = *input.Form
	}
	if err := validate(&current); err != nil {
		return Medication{}, err
	}
	if err := service.repository.Update(ctx, current); err != nil {
		return Medication{}, fmt.Errorf("update medication: %w", err)
	}

	return current, nil
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

func validateField(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
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
