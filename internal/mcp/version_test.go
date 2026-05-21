package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/pockyHM/conan/pkg/mcpproto"
)

func TestCheckVersionsAllMatch(t *testing.T) {
	clientA := newVersionTestClient(t, "1.2.3", nil)
	clientB := newVersionTestClient(t, "1.2.3", nil)

	results := CheckVersions(context.Background(), map[string]*Client{
		"node-b": clientB,
		"node-a": clientA,
	})

	sort.Slice(results, func(i, j int) bool { return results[i].Node < results[j].Node })
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.Error != nil {
			t.Fatalf("%s error = %v, want nil", result.Node, result.Error)
		}
		if result.Version != "1.2.3" {
			t.Fatalf("%s version = %q, want 1.2.3", result.Node, result.Version)
		}
	}

	mismatches := CheckVersionMismatches("1.2.3", results)
	if len(mismatches) != 0 {
		t.Fatalf("mismatches = %#v, want none", mismatches)
	}
}

func TestCheckVersionMismatchesReportsDifferentAgentVersion(t *testing.T) {
	mismatches := CheckVersionMismatches("1.2.3", []VersionResult{
		{Node: "node-a", Version: "1.2.4"},
	})

	if len(mismatches) != 1 {
		t.Fatalf("len(mismatches) = %d, want 1", len(mismatches))
	}
	if mismatches[0] != (Mismatch{Node: "node-a", Got: "1.2.4", Expected: "1.2.3"}) {
		t.Fatalf("mismatch = %#v", mismatches[0])
	}
}

func TestCheckVersionMismatchesReportsErrorResult(t *testing.T) {
	err := errors.New("connection refused")
	mismatches := CheckVersionMismatches("1.2.3", []VersionResult{
		{Node: "node-a", Error: err},
	})

	if len(mismatches) != 1 {
		t.Fatalf("len(mismatches) = %d, want 1", len(mismatches))
	}
	if mismatches[0].Node != "node-a" || mismatches[0].Got != "connection refused" || mismatches[0].Expected != "1.2.3" || !mismatches[0].IsError {
		t.Fatalf("mismatch = %#v", mismatches[0])
	}
}

func TestCheckVersionMismatchesCLIDevIgnoresMismatches(t *testing.T) {
	mismatches := CheckVersionMismatches("dev", []VersionResult{
		{Node: "node-a", Version: "1.2.4"},
		{Node: "node-b", Error: errors.New("connection refused")},
	})

	if len(mismatches) != 0 {
		t.Fatalf("mismatches = %#v, want none", mismatches)
	}
}

func TestCheckVersionMismatchesAgentDevIgnored(t *testing.T) {
	mismatches := CheckVersionMismatches("1.2.3", []VersionResult{
		{Node: "node-a", Version: "dev"},
	})

	if len(mismatches) != 0 {
		t.Fatalf("mismatches = %#v, want none", mismatches)
	}
}

func TestCheckVersionsCollectsInitializeErrors(t *testing.T) {
	client := newVersionTestClient(t, "", errors.New("agent unavailable"))

	results := CheckVersions(context.Background(), map[string]*Client{"node-a": client})

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Node != "node-a" {
		t.Fatalf("node = %q, want node-a", results[0].Node)
	}
	if results[0].Error == nil || !strings.Contains(results[0].Error.Error(), "agent unavailable") {
		t.Fatalf("error = %v, want agent unavailable", results[0].Error)
	}
}

func newVersionTestClient(t *testing.T, version string, requestErr error) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestErr != nil {
			http.Error(w, requestErr.Error(), http.StatusServiceUnavailable)
			return
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Method != "initialize" {
			t.Fatalf("method = %q, want initialize", req.Method)
		}
		writeRPC(t, w, req.ID, mcpproto.InitializeResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo:      mcpproto.ServerInfo{Name: "conan-agent", Version: version},
		})
	}))
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL})
}
