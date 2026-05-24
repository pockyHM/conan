package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewID(t *testing.T) {
	id1 := NewID()
	id2 := NewID()
	if id1 == "" {
		t.Error("id should not be empty")
	}
	if id1 == id2 {
		t.Error("ids should be unique")
	}
}

func TestNewSessionIDReturnsFullUUID(t *testing.T) {
	id1 := NewSessionID()
	id2 := NewSessionID()
	if id1 == "" {
		t.Error("session id should not be empty")
	}
	if id1 == id2 {
		t.Error("session ids should be unique")
	}
	if _, err := uuid.Parse(id1); err != nil {
		t.Fatalf("session id %q is not a valid UUID: %v", id1, err)
	}
	if len(id1) != 36 {
		t.Fatalf("session id length = %d, want 36", len(id1))
	}
}

func TestMemoryCategories(t *testing.T) {
	cats := []string{CategoryEvent, CategoryExperience, CategoryTroubleshooting, CategoryTopology}
	for _, cat := range cats {
		if cat == "" {
			t.Error("category should not be empty")
		}
	}
}
