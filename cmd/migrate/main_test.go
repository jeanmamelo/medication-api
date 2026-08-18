package main

import (
	"testing"

	"github.com/jeanmamelo/medication-api/migrations"
)

func TestLoadMigrations(t *testing.T) {
	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("loadMigrations() returned %d migrations, want 1", len(items))
	}
	if items[0].version != 1 || items[0].name != "000001_create_medications.up.sql" {
		t.Errorf("migration = %#v", items[0])
	}
	if _, err := migrations.FS.ReadFile(items[0].path); err != nil {
		t.Fatalf("embedded migration is unreadable: %v", err)
	}
}
