package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditWritesAllowWithToolInputAndNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Tool:  "shell_run",
		Input: `{"command":"uptime"}`,
		Risk:  "ALLOW",
		Nodes: []string{"node-01"},
	})

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	line := string(contents)
	for _, want := range []string{"[ALLOW]", "shell_run", "node-01", `"{\"command\":\"uptime\"}"`, `"risk":"ALLOW"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("audit line missing %q: %s", want, line)
		}
	}
}

func TestAuditCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "logs", "audit.log")
	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	logger.Close()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("audit log file was not created: %v", err)
	}
}

func TestAuditCreatesLogFilePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat audit log: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("audit log permissions = %o, want 0600", got)
	}
}

func TestAuditClosePreventsFurtherWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	logger.Log(AuditEntry{Tool: "shell_run", Input: `{}`, Risk: "ALLOW"})
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	logger.Log(AuditEntry{Tool: "shell_run", Input: `{}`, Risk: "DENY"})

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if strings.Contains(string(contents), "[DENY]") {
		t.Fatalf("Log wrote after Close: %s", contents)
	}
}

func TestAuditDenyIncludesDeny(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Tool:   "shell_run",
		Input:  `{"command":"rm -rf /"}`,
		Risk:   "DENY",
		Reason: "destructive",
		Nodes:  []string{"node-01"},
	})

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	line := string(contents)
	for _, want := range []string{"[DENY]", "DENY", "destructive"} {
		if !strings.Contains(line, want) {
			t.Fatalf("audit line missing %q: %s", want, line)
		}
	}
}

func TestAuditConfirmApprovedIncludesConfirmAndOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer logger.Close()

	logger.Log(AuditEntry{
		Tool:    "svc_restart",
		Input:   `{"name":"nginx"}`,
		Risk:    "CONFIRM",
		Outcome: "approved",
		Reason:  "service restart",
		Nodes:   []string{"node-01"},
	})

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	line := string(contents)
	for _, want := range []string{"[CONFIRM]", "outcome=\"approved\"", `"outcome":"approved"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("audit line missing %q: %s", want, line)
		}
	}
}
