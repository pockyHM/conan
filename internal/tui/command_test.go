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
