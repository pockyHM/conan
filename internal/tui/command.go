package tui

import "strings"

type CommandKind string

const (
	CommandHelp    CommandKind = "help"
	CommandClear   CommandKind = "clear"
	CommandExit    CommandKind = "exit"
	CommandCluster CommandKind = "cluster"
	CommandModel   CommandKind = "model"
	CommandNodes   CommandKind = "nodes"
	CommandUnknown CommandKind = "unknown"
)

type SlashCommand struct {
	Kind CommandKind
	Arg  string
}

func ParseSlashCommand(input string) (SlashCommand, bool) {
	text := strings.TrimSpace(input)
	if !strings.HasPrefix(text, "/") {
		return SlashCommand{}, false
	}

	text = strings.TrimSpace(strings.TrimPrefix(text, "/"))
	if text == "" {
		return SlashCommand{}, false
	}
	name := text
	arg := ""
	if fields := strings.Fields(text); len(fields) > 0 {
		name = fields[0]
		if len(fields) > 1 {
			arg = strings.Join(fields[1:], " ")
		}
	}

	switch name {
	case "help":
		return SlashCommand{Kind: CommandHelp, Arg: arg}, true
	case "clear":
		return SlashCommand{Kind: CommandClear, Arg: arg}, true
	case "exit":
		return SlashCommand{Kind: CommandExit, Arg: arg}, true
	case "cluster":
		return SlashCommand{Kind: CommandCluster, Arg: arg}, true
	case "model":
		return SlashCommand{Kind: CommandModel, Arg: arg}, true
	case "nodes":
		return SlashCommand{Kind: CommandNodes, Arg: arg}, true
	default:
		if arg != "" {
			return SlashCommand{Kind: CommandUnknown, Arg: name + " " + arg}, true
		}
		return SlashCommand{Kind: CommandUnknown, Arg: name}, true
	}
}
