package models

import "testing"

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

func TestMemoryCategories(t *testing.T) {
	cats := []string{CategoryEvent, CategoryExperience, CategoryTroubleshooting, CategoryTopology}
	for _, cat := range cats {
		if cat == "" {
			t.Error("category should not be empty")
		}
	}
}
