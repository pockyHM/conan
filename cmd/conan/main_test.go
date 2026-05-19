package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestTUICommandRegistered(t *testing.T) {
	stdout, _, err := executeCommand("tui", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(stdout, "Start the interactive TUI") {
		t.Fatalf("help output = %q", stdout)
	}
}

func TestTUICommandUsesConfiguredStreams(t *testing.T) {
	oldRun := runTeaProgram
	defer func() { runTeaProgram = oldRun }()

	input := strings.NewReader("")
	var output bytes.Buffer
	called := false
	runTeaProgram = func(model tea.Model, in io.Reader, out io.Writer) error {
		called = true
		if in != input {
			t.Fatalf("input stream = %T, want configured input", in)
		}
		if out != &output {
			t.Fatalf("output stream = %T, want configured output", out)
		}
		return nil
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
