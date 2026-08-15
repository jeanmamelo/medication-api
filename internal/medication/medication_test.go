package medication

import (
	"context"
	"errors"
	"testing"
)

func TestCreateValidatesAndNormalizesInput(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository, fixedID("018f2f15-0cf5-7e54-9ecb-65df2a99e422"))

	got, err := service.Create(context.Background(), CreateInput{Name: " Paracetamol ", Dosage: " 500 mg ", Form: " tablet "})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.Name != "Paracetamol" || got.Dosage != "500 mg" || got.Form != "tablet" {
		t.Errorf("Create() = %#v", got)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	service := NewService(&memoryRepository{}, fixedID("id"))
	if _, err := service.Create(context.Background(), CreateInput{Name: "", Dosage: "500 mg", Form: "tablet"}); err == nil {
		t.Fatal("Create() error = nil, want validation error")
	}
}

func TestUpdateOnlyChangesSuppliedFields(t *testing.T) {
	repository := &memoryRepository{medications: map[string]Medication{"id": {ID: "id", Name: "Paracetamol", Dosage: "500 mg", Form: "tablet"}}}
	service := NewService(repository, fixedID("unused"))
	name := "Ibuprofen"

	got, err := service.Update(context.Background(), "id", UpdateInput{Name: &name})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got.Name != "Ibuprofen" || got.Dosage != "500 mg" || got.Form != "tablet" {
		t.Errorf("Update() = %#v", got)
	}
}

func TestListRejectsUnboundedPagination(t *testing.T) {
	service := NewService(&memoryRepository{}, fixedID("unused"))
	if _, err := service.List(context.Background(), 101, 0); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("List() error = %v, want ErrInvalidPagination", err)
	}
}

type fixedID string

func (id fixedID) New() (string, error) { return string(id), nil }

type memoryRepository struct{ medications map[string]Medication }

func (repository *memoryRepository) Create(_ context.Context, medication Medication) error {
	if repository.medications == nil {
		repository.medications = make(map[string]Medication)
	}
	repository.medications[medication.ID] = medication
	return nil
}
func (repository *memoryRepository) Get(_ context.Context, id string) (Medication, error) {
	medication, ok := repository.medications[id]
	if !ok {
		return Medication{}, ErrNotFound
	}
	return medication, nil
}
func (repository *memoryRepository) List(_ context.Context, _, _ int) ([]Medication, error) {
	return nil, nil
}
func (repository *memoryRepository) Update(_ context.Context, medication Medication) error {
	repository.medications[medication.ID] = medication
	return nil
}
func (repository *memoryRepository) Delete(_ context.Context, id string) error {
	if _, ok := repository.medications[id]; !ok {
		return ErrNotFound
	}
	delete(repository.medications, id)
	return nil
}
