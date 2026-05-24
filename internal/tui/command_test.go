package tui

import "testing"

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input string
		kind  CommandKind
		arg   string
	}{
		{input: "/help", kind: CommandHelp},
		{input: "/clear", kind: CommandClear},
		{input: "/exit", kind: CommandExit},
		{input: "/cluster production", kind: CommandCluster, arg: "production"},
		{input: "/config", kind: CommandConfig},
		{input: "/lang", kind: CommandLang},
		{input: "/language zh", kind: CommandLang, arg: "zh"},
		{input: "/model claude-sonnet", kind: CommandModel, arg: "claude-sonnet"},
		{input: "/node", kind: CommandNode},
		{input: "/node off", kind: CommandNode, arg: "off"},
		{input: "/nodes", kind: CommandNodes},
		{input: "/memory", kind: CommandMemory},
		{input: "/resume", kind: CommandResume},
		{input: "/resume abc123", kind: CommandResume, arg: "abc123"},
		{input: "/compact", kind: CommandCompact},
		{input: "/compact keep deployment decisions", kind: CommandCompact, arg: "keep deployment decisions"},
		{input: "/thinking 你好", kind: CommandThinking, arg: "你好"},
		{input: "/agent investigator check cpu", kind: CommandAgent, arg: "investigator check cpu"},
		{input: "/subagents on", kind: CommandSubagents, arg: "on"},
		{input: "/agents", kind: CommandSubagents},
		{input: "/incident start API latency", kind: CommandIncident, arg: "start API latency"},
		{input: "/incident status", kind: CommandIncident, arg: "status"},
		{input: "/incident note checked nginx logs", kind: CommandIncident, arg: "note checked nginx logs"},
		{input: "/incident export", kind: CommandIncident, arg: "export"},
		{input: "/incident close", kind: CommandIncident, arg: "close"},
		{input: "/runbook draft", kind: CommandRunbook, arg: "draft"},
		{input: "/runbook draft incidents/2026-05-23-api.md", kind: CommandRunbook, arg: "draft incidents/2026-05-23-api.md"},
		{input: "/runbook preview runbooks/2026-05-23-nginx-502.md", kind: CommandRunbook, arg: "preview runbooks/2026-05-23-nginx-502.md"},
		{input: "/runbook run runbooks/2026-05-23-nginx-502.md", kind: CommandRunbook, arg: "run runbooks/2026-05-23-nginx-502.md"},
	}

	for _, tt := range tests {
		cmd, ok := ParseSlashCommand(tt.input)
		if !ok {
			t.Fatalf("ParseSlashCommand(%q) ok=false", tt.input)
		}
		if cmd.Kind != tt.kind || cmd.Arg != tt.arg {
			t.Fatalf("ParseSlashCommand(%q) = %#v", tt.input, cmd)
		}
	}
}

func TestParseSkillsCommands(t *testing.T) {
	tests := []struct {
		input string
		kind  CommandKind
		arg   string
	}{
		{input: "/skills", kind: CommandSkills},
		{input: "/skills install github.com/org/repo", kind: CommandSkills, arg: "install github.com/org/repo"},
		{input: "/skill k8s-debug pods failing", kind: CommandSkill, arg: "k8s-debug pods failing"},
	}

	for _, tt := range tests {
		cmd, ok := ParseSlashCommand(tt.input)
		if !ok {
			t.Fatalf("ParseSlashCommand(%q) ok=false", tt.input)
		}
		if cmd.Kind != tt.kind || cmd.Arg != tt.arg {
			t.Fatalf("ParseSlashCommand(%q) = %#v", tt.input, cmd)
		}
	}
}

func TestParseSlashCommandRejectsNormalText(t *testing.T) {
	if _, ok := ParseSlashCommand("hello"); ok {
		t.Fatal("normal text should not parse as slash command")
	}
}

func TestParseSlashCommandUnknown(t *testing.T) {
	cmd, ok := ParseSlashCommand("/unknown value")
	if !ok {
		t.Fatal("slash input should parse")
	}
	if cmd.Kind != CommandUnknown || cmd.Arg != "unknown value" {
		t.Fatalf("unknown command = %#v", cmd)
	}
}

func TestParseSlashCommandSeparatesOnAnyWhitespace(t *testing.T) {
	cmd, ok := ParseSlashCommand("/cluster\tproduction")
	if !ok {
		t.Fatal("slash input should parse")
	}
	if cmd.Kind != CommandCluster || cmd.Arg != "production" {
		t.Fatalf("tab separated command = %#v", cmd)
	}
}

func TestParseSlashCommandRejectsEmptyCommand(t *testing.T) {
	if _, ok := ParseSlashCommand("/"); ok {
		t.Fatal("empty slash command should not parse")
	}
	if _, ok := ParseSlashCommand("/   "); ok {
		t.Fatal("whitespace-only slash command should not parse")
	}
}
