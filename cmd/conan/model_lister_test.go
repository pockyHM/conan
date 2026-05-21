package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAIModelListerParsesModels(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []map[string]string{
				{"id": "model-b"},
				{"id": ""},
				{"id": "model-a"},
			},
		})
	}))
	defer srv.Close()

	lister := OpenAIModelLister{Client: &http.Client{Timeout: 5 * time.Second}}
	models, err := lister.ListModels(context.Background(), srv.URL, "sk-test")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q, want Bearer sk-test", gotAuth)
	}
	if len(models) != 2 || models[0] != "model-a" || models[1] != "model-b" {
		t.Fatalf("models = %v, want [model-a model-b]", models)
	}
}

func TestOpenAIModelListerReturnsErrorForBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	lister := OpenAIModelLister{Client: &http.Client{Timeout: 5 * time.Second}}
	_, err := lister.ListModels(context.Background(), srv.URL, "key")
	if err == nil {
		t.Fatal("expected error for 503")
	}
}

func TestOpenAIModelListerReturnsErrorForMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	lister := OpenAIModelLister{Client: &http.Client{Timeout: 5 * time.Second}}
	_, err := lister.ListModels(context.Background(), srv.URL, "key")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}
