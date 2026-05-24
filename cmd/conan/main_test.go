package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pockyHM/conan/internal/conversation"
	"github.com/pockyHM/conan/internal/logging"
	"github.com/pockyHM/conan/internal/memory"
	"github.com/pockyHM/conan/internal/skills"
	"github.com/pockyHM/conan/pkg/mcpproto"
	"github.com/pockyHM/conan/pkg/models"
)

func executeCommand(args ...string) (string, string, error) {
	cmd := newRootCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestNoArgCommandsRejectExtraArgs(t *testing.T) {
	_, _, err := executeCommand("clusters", "unexpected")
	if err == nil || !strings.Contains(err.Error(), "unknown command") && !strings.Contains(err.Error(), "accepts 0 arg") {
		t.Fatalf("err = %v", err)
	}
}

func TestToolsListHelpShowsRequiredNode(t *testing.T) {
	stdout, _, err := executeCommand("tools", "list", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(stdout, "list <node>") {
		t.Fatalf("help output = %q", stdout)
	}
}

func TestFilesPutUploadsLocalFileToAgent(t *testing.T) {
	var uploadedPath string
	var uploadedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/files/upload" {
			t.Fatalf("request = %s %s, want PUT /files/upload", r.Method, r.URL.Path)
		}
		uploadedPath = r.URL.Query().Get("path")
		var err error
		uploadedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upload body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	home := writeSingleNodeHome(t, srv.URL)
	local := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(local, []byte("hello world"), 0644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	stdout, _, err := executeCommand("--home", home, "files", "put", "node-a", local, "/remote/file.txt")
	if err != nil {
		t.Fatalf("files put: %v", err)
	}
	if !strings.Contains(stdout, "node-a\tuploaded\t/remote/file.txt\t11 bytes") {
		t.Fatalf("stdout = %q", stdout)
	}
	if uploadedPath != "/remote/file.txt" || string(uploadedBody) != "hello world" {
		t.Fatalf("uploaded path=%q body=%q", uploadedPath, uploadedBody)
	}
}

func TestFilesGetDownloadsAgentFileToLocalPath(t *testing.T) {
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/files/download" {
			t.Fatalf("request = %s %s, want GET /files/download", r.Method, r.URL.Path)
		}
		requestedPath = r.URL.Query().Get("path")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	home := writeSingleNodeHome(t, srv.URL)
	local := filepath.Join(t.TempDir(), "downloaded", "file.txt")

	stdout, _, err := executeCommand("--home", home, "files", "get", "node-a", "/remote/file.txt", local)
	if err != nil {
		t.Fatalf("files get: %v", err)
	}
	if !strings.Contains(stdout, "node-a\tdownloaded\t/remote/file.txt\t11 bytes") {
		t.Fatalf("stdout = %q", stdout)
	}
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("local data = %q", data)
	}
	if requestedPath != "/remote/file.txt" {
		t.Fatalf("requested path = %q", requestedPath)
	}
}

func TestSkillsListEmpty(t *testing.T) {
	home := t.TempDir()

	stdout, _, err := executeCommand("--home", home, "skills", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "No skills installed") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSkillsListUsesDefaultCluster(t *testing.T) {
	home := t.TempDir()
	clusterDir := filepath.Join(home, "clusters", "prod")
	if err := os.MkdirAll(clusterDir, 0755); err != nil {
		t.Fatalf("mkdir cluster: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("default_cluster: prod\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	registry := `skills:
  - name: k8s-debug
    description: Diagnose Kubernetes failures.
    source: github.com/acme/ops
    ref: main
    path: skills/k8s-debug
    cache_path: skills/repos/github.com/acme/ops/main/skills/k8s-debug
`
	if err := os.WriteFile(filepath.Join(clusterDir, "skills.yaml"), []byte(registry), 0644); err != nil {
		t.Fatalf("write skills: %v", err)
	}

	stdout, _, err := executeCommand("--home", home, "skills", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "cluster:prod") || !strings.Contains(stdout, "k8s-debug") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSkillsInstallRejectsInvalidGitHubRepo(t *testing.T) {
	home := t.TempDir()

	_, _, err := executeCommand("--home", home, "skills", "install", "not-a-valid-source", "--global")
	if err == nil {
		t.Fatal("err = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "invalid GitHub repository") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkillsRemoveGlobal(t *testing.T) {
	home := t.TempDir()
	reg := skills.Registry{Skills: []skills.RegistryEntry{{
		Name: "k8s-debug", Description: "Diagnose Kubernetes failures.", Source: "github.com/acme/ops", Ref: "main", Path: "skills/k8s-debug", CachePath: "skills/repos/github.com/acme/ops/main/skills/k8s-debug",
	}}}
	if err := skills.SaveRegistry(skills.GlobalRegistryPath(home), reg); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := executeCommand("--home", home, "skills", "remove", "k8s-debug", "--global")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "removed k8s-debug") {
		t.Fatalf("stdout = %q", stdout)
	}
	got, err := skills.LoadRegistry(skills.GlobalRegistryPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 0 {
		t.Fatalf("registry = %#v, want empty", got)
	}
}

func TestSkillsUpdateCommandRegistered(t *testing.T) {
	stdout, _, err := executeCommand("skills", "update", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(stdout, "update [name]") {
		t.Fatalf("help output = %q", stdout)
	}
}

func TestTUICommandRegistered(t *testing.T) {
	stdout, _, err := executeCommand("tui", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(stdout, "Start the interactive TUI") {
		t.Fatalf("help output = %q", stdout)
	}
}

func TestResumeCommandRegistered(t *testing.T) {
	stdout, _, err := executeCommand("resume", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(stdout, "resume <id>") {
		t.Fatalf("help output = %q", stdout)
	}
}

func writeSingleNodeHome(t *testing.T, serverURL string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "clusters", "test"), 0755); err != nil {
		t.Fatalf("mkdir cluster: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("default_cluster: test\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "clusters", "test", "cluster.yaml"), []byte("name: test\n"), 0644); err != nil {
		t.Fatalf("write cluster: %v", err)
	}
	portText := serverURL[strings.LastIndex(serverURL, ":")+1:]
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse server port: %v", err)
	}
	nodes := "nodes:\n  - name: node-a\n    host: 127.0.0.1\n    agent:\n      port: " + strconv.Itoa(port) + "\n"
	if err := os.WriteFile(filepath.Join(home, "clusters", "test", "nodes.yaml"), []byte(nodes), 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}
	return home
}

func TestResumeCommandLoadsSessionInTUI(t *testing.T) {
	oldRun := runTeaProgram
	defer func() { runTeaProgram = oldRun }()
	defer logging.Close()

	home := t.TempDir()
	store, err := memory.Open(filepath.Join(home, "memory"))
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()

	saved := []models.Message{{ID: "m1", ConversationID: "conv-resume", Role: conversation.RoleUser, Content: "restored compact context"}}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	if err := store.SaveConversation(memory.ConversationRecord{
		ID:       "conv-resume",
		Cluster:  "prod",
		Model:    "model",
		Messages: string(data),
	}); err != nil {
		t.Fatalf("save conversation: %v", err)
	}

	called := false
	runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
		called = true
		cmd := model.Init()
		if cmd == nil {
			t.Fatal("Init() returned nil, want initial resume load command")
		}
		model = applyTeaCommandForTest(t, model, cmd)
		if !strings.Contains(model.View(), "restored compact context") {
			t.Fatalf("resumed view missing saved message:\n%s", model.View())
		}
		return model, nil
	}

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "resume", "conv-resume"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !called {
		t.Fatal("resume command did not start TUI")
	}
}

func TestTUICommandPrintsResumeHintAfterExit(t *testing.T) {
	oldRun := runTeaProgram
	defer func() { runTeaProgram = oldRun }()
	defer logging.Close()

	home := t.TempDir()
	var output bytes.Buffer
	runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
		return model, nil
	}

	cmd := newRootCommand()
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--home", home, "tui"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("tui: %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "Session saved: ") || !strings.Contains(got, "Resume with: conan resume ") {
		t.Fatalf("output missing resume hint:\n%s", got)
	}
}

func applyTeaCommandForTest(t *testing.T, model tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c == nil {
				continue
			}
			next, _ := model.Update(c())
			model = next
		}
		return model
	}
	next, _ := model.Update(msg)
	return next
}

func TestTUICommandUsesConfiguredStreams(t *testing.T) {
	oldRun := runTeaProgram
	defer func() { runTeaProgram = oldRun }()
	defer logging.Close()

	input := strings.NewReader("")
	var output bytes.Buffer
	called := false
	runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
		called = true
		if in != input {
			t.Fatalf("input stream = %T, want configured input", in)
		}
		if out != &output {
			t.Fatalf("output stream = %T, want configured output", out)
		}
		return model, nil
	}

	cmd := newRootCommand()
	cmd.SetIn(input)
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"tui"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("tui: %v", err)
	}
	if !called {
		t.Fatal("tui program was not started")
	}
}

func TestTUIProgramOptionsEnableMouseCellMotion(t *testing.T) {
	input := strings.NewReader("")
	var output bytes.Buffer
	program := tea.NewProgram(nil, teaProgramOptions(input, &output)...)

	startupOptions := reflect.ValueOf(program).Elem().FieldByName("startupOptions").Int()
	const (
		withMouseCellMotion = int64(1 << 1)
		withMouseAllMotion  = int64(1 << 2)
	)
	if startupOptions&withMouseCellMotion == 0 {
		t.Fatal("tui should enable mouse cell motion so wheel events are captured")
	}
	if startupOptions&withMouseAllMotion != 0 {
		t.Fatal("tui should not enable full mouse motion")
	}
}

func TestTUICommandInitializesConfiguredLogging(t *testing.T) {
	oldRun := runTeaProgram
	defer func() { runTeaProgram = oldRun }()

	home := t.TempDir()
	logFile := filepath.Join(home, "logs", "conan.jsonl")
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("logging:\n  level: debug\n  file: "+logFile+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
		logging.Write("tui logging initialized")
		return model, nil
	}

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "tui"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("tui: %v", err)
	}

	contents, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(contents), "tui logging initialized") {
		t.Fatalf("log file = %q, want tui log message", contents)
	}
}

func TestTUICommandInitializesAuditLogger(t *testing.T) {
	oldRun := runTeaProgram
	defer func() { runTeaProgram = oldRun }()

	home := t.TempDir()
	logFile := filepath.Join(home, "logs", "conan.jsonl")
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("logging:\n  audit: true\n  file: "+logFile+"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
		return model, nil
	}

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "tui"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("tui: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, "logs", "audit.log")); err != nil {
		t.Fatalf("audit log was not created next to configured log file: %v", err)
	}
}

func TestTUICommandInitializesLoggingWithEmptyFile(t *testing.T) {
	oldRun := runTeaProgram
	defer func() { runTeaProgram = oldRun }()
	defer logging.Close()

	home := t.TempDir()
	previousLogFile := filepath.Join(home, "previous.jsonl")
	if err := logging.Setup(logging.Config{File: previousLogFile}); err != nil {
		t.Fatalf("setup previous logger: %v", err)
	}
	logging.Write("before tui")

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("logging:\n  level: debug\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
		logging.Write("discarded tui log")
		return model, nil
	}

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "tui"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("tui: %v", err)
	}

	contents, err := os.ReadFile(previousLogFile)
	if err != nil {
		t.Fatalf("read previous log file: %v", err)
	}
	if strings.Contains(string(contents), "discarded tui log") {
		t.Fatalf("empty logging file did not reinitialize logger to discard: %q", contents)
	}
}

func TestTUICommandInitChecksAgentVersionsAndRendersWarning(t *testing.T) {
	oldRun := runTeaProgram
	oldVersion := version
	defer func() {
		runTeaProgram = oldRun
		version = oldVersion
	}()
	defer logging.Close()

	version = "1.2.3"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
			t.Fatalf("request = %s %s, want POST /rpc", r.Method, r.URL.Path)
		}
		var req mcpproto.JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "tools/list":
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, map[string]interface{}{"tools": []interface{}{}}))
		case "initialize":
			_ = json.NewEncoder(w).Encode(mcpproto.NewSuccessResponse(req.ID, mcpproto.InitializeResult{
				ProtocolVersion: "2024-11-05",
				ServerInfo:      mcpproto.ServerInfo{Name: "conan-agent", Version: "1.2.2"},
			}))
		default:
			t.Fatalf("method = %q, want tools/list or initialize", req.Method)
		}
	}))
	defer srv.Close()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "clusters", "test"), 0755); err != nil {
		t.Fatalf("mkdir cluster: %v", err)
	}
	config := "default_cluster: test\nlogging:\n  level: debug\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(config), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cluster := "agent:\n  listen: 127.0.0.1:9280\n"
	if err := os.WriteFile(filepath.Join(home, "clusters", "test", "cluster.yaml"), []byte(cluster), 0644); err != nil {
		t.Fatalf("write cluster: %v", err)
	}
	nodes := "nodes:\n  - name: node-a\n    host: 127.0.0.1\n    agent:\n      port: " + strings.TrimPrefix(srv.URL, "http://127.0.0.1:") + "\n"
	if err := os.WriteFile(filepath.Join(home, "clusters", "test", "nodes.yaml"), []byte(nodes), 0644); err != nil {
		t.Fatalf("write nodes: %v", err)
	}

	runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) (tea.Model, error) {
		cmd := model.Init()
		if cmd == nil {
			t.Fatal("Init() returned nil, want version check command")
		}
		msg := cmd()
		// Init batches tool fetch with version check; execute all and apply
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				inner := c()
				next, _ := model.Update(inner)
				model = next
			}
		} else {
			model.Update(msg)
		}
		view := model.View()
		if !strings.Contains(view, "Version warning") || !strings.Contains(view, "node-a: 1.2.2 (expected 1.2.3)") {
			t.Fatalf("view missing version warning:\n%s", view)
		}
		return model, nil
	}

	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "tui"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("tui: %v", err)
	}
}

func TestNodeAddCommandRegistered(t *testing.T) {
	stdout, _, err := executeCommand("node", "add", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(stdout, "add <hostname-or-ip>") {
		t.Fatalf("help output = %q", stdout)
	}
	if !strings.Contains(stdout, "--no-deploy") || !strings.Contains(stdout, "--rotate-token") {
		t.Fatalf("help output missing node add flags: %q", stdout)
	}
}

func TestNodeAddNoDeployWritesConfig(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "clusters", "prod"), 0755); err != nil {
		t.Fatalf("mkdir cluster: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("default_cluster: prod\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "clusters", "prod", "cluster.yaml"), []byte("name: prod\n"), 0644); err != nil {
		t.Fatalf("write cluster: %v", err)
	}

	stdout, _, err := executeCommand("--home", home, "node", "add", "127.0.0.1", "--no-deploy", "--port", "9300")
	if err != nil {
		t.Fatalf("node add: %v", err)
	}
	if !strings.Contains(stdout, "node added: 127.0.0.1") {
		t.Fatalf("stdout = %q", stdout)
	}
	data, err := os.ReadFile(filepath.Join(home, "clusters", "prod", "nodes.yaml"))
	if err != nil {
		t.Fatalf("read nodes.yaml: %v", err)
	}
	contents := string(data)
	for _, want := range []string{"name: 127.0.0.1", "host: 127.0.0.1", "port: 9300", "token:"} {
		if !strings.Contains(contents, want) {
			t.Fatalf("nodes.yaml missing %q:\n%s", want, contents)
		}
	}
}

func TestNodeAddAutoCreatesDefaultCluster(t *testing.T) {
	home := t.TempDir()

	stdout, _, err := executeCommand("--home", home, "node", "add", "10.0.0.1", "--no-deploy")
	if err != nil {
		t.Fatalf("node add: %v", err)
	}
	if !strings.Contains(stdout, "node added: 10.0.0.1") {
		t.Fatalf("stdout = %q", stdout)
	}
	clusterYAML := filepath.Join(home, "clusters", "default", "cluster.yaml")
	if _, err := os.Stat(clusterYAML); err != nil {
		t.Fatalf("default cluster.yaml not created: %v", err)
	}
	nodesYAML := filepath.Join(home, "clusters", "default", "nodes.yaml")
	data, err := os.ReadFile(nodesYAML)
	if err != nil {
		t.Fatalf("read nodes.yaml: %v", err)
	}
	contents := string(data)
	for _, want := range []string{"name: 10.0.0.1", "host: 10.0.0.1"} {
		if !strings.Contains(contents, want) {
			t.Fatalf("nodes.yaml missing %q:\n%s", want, contents)
		}
	}
}
