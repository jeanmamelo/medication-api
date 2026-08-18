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
	name := " Ibuprofen "

	got, err := service.Update(context.Background(), "id", UpdateInput{Name: &name})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got.Name != "Ibuprofen" || got.Dosage != "500 mg" || got.Form != "tablet" {
		t.Errorf("Update() = %#v", got)
	}
}

func TestUpdateRejectsBlankSuppliedField(t *testing.T) {
	repository := &memoryRepository{medications: map[string]Medication{"id": {ID: "id", Name: "Paracetamol", Dosage: "500 mg", Form: "tablet"}}}
	service := NewService(repository, fixedID("unused"))
	blank := "   "

	_, err := service.Update(context.Background(), "id", UpdateInput{Dosage: &blank})
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != "dosage" {
		t.Fatalf("Update() error = %v, want *ValidationError for dosage", err)
	}
	if !errors.Is(err, ErrValidation) {
		t.Errorf("Update() error does not match ErrValidation: %v", err)
	}
	if stored := repository.medications["id"]; stored.Dosage != "500 mg" {
		t.Errorf("Update() wrote an invalid value: %#v", stored)
	}
}

func TestUpdatePropagatesNotFoundUnwrapped(t *testing.T) {
	service := NewService(&memoryRepository{}, fixedID("unused"))
	name := "Ibuprofen"

	if _, err := service.Update(context.Background(), "missing", UpdateInput{Name: &name}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update() error = %v, want ErrNotFound", err)
	}
}

func TestValidationErrorMatchesSentinel(t *testing.T) {
	err := validateField("name", "")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("validateField() error = %v, want ErrValidation", err)
	}
	if err.Error() != "name is required" {
		t.Errorf("validateField() message = %q", err.Error())
	}
}

func TestListRejectsUnboundedPagination(t *testing.T) {
	service := NewService(&memoryRepository{}, fixedID("unused"))
	if _, err := service.List(context.Background(), MaxPageSize+1, 0); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("List() error = %v, want ErrInvalidPagination", err)
	}
}

func TestValidatePagination(t *testing.T) {
	for _, test := range []struct {
		limit, offset int
		wantErr       bool
	}{
		{limit: 1, offset: 0},
		{limit: MaxPageSize, offset: 5},
		{limit: 0, offset: 0, wantErr: true},
		{limit: MaxPageSize + 1, offset: 0, wantErr: true},
		{limit: 20, offset: -1, wantErr: true},
	} {
		err := ValidatePagination(test.limit, test.offset)
		if (err != nil) != test.wantErr {
			t.Errorf("ValidatePagination(%d, %d) error = %v, wantErr = %v", test.limit, test.offset, err, test.wantErr)
		}
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
func (repository *memoryRepository) Update(_ context.Context, id string, input UpdateInput) (Medication, error) {
	current, ok := repository.medications[id]
	if !ok {
		return Medication{}, ErrNotFound
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
	repository.medications[id] = current
	return current, nil
}
func (repository *memoryRepository) Delete(_ context.Context, id string) error {
	if _, ok := repository.medications[id]; !ok {
		return ErrNotFound
	}
	delete(repository.medications, id)
	return nil
}
