package tui

import "strings"

type CommandKind string

const (
	CommandHelp      CommandKind = "help"
	CommandClear     CommandKind = "clear"
	CommandCompact   CommandKind = "compact"
	CommandExit      CommandKind = "exit"
	CommandCluster   CommandKind = "cluster"
	CommandConfig    CommandKind = "config"
	CommandSkills    CommandKind = "skills"
	CommandSkill     CommandKind = "skill"
	CommandLang      CommandKind = "lang"
	CommandModel     CommandKind = "model"
	CommandNode      CommandKind = "node"
	CommandNodes     CommandKind = "nodes"
	CommandMemory    CommandKind = "memory"
	CommandResume    CommandKind = "resume"
	CommandThinking  CommandKind = "thinking"
	CommandAgent     CommandKind = "agent"
	CommandSubagents CommandKind = "subagents"
	CommandUnknown   CommandKind = "unknown"
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
	case "compact":
		return SlashCommand{Kind: CommandCompact, Arg: arg}, true
	case "exit":
		return SlashCommand{Kind: CommandExit, Arg: arg}, true
	case "cluster":
		return SlashCommand{Kind: CommandCluster, Arg: arg}, true
	case "config":
		return SlashCommand{Kind: CommandConfig, Arg: arg}, true
	case "skills":
		return SlashCommand{Kind: CommandSkills, Arg: arg}, true
	case "skill":
		return SlashCommand{Kind: CommandSkill, Arg: arg}, true
	case "lang", "language":
		return SlashCommand{Kind: CommandLang, Arg: arg}, true
	case "model":
		return SlashCommand{Kind: CommandModel, Arg: arg}, true
	case "node":
		return SlashCommand{Kind: CommandNode, Arg: arg}, true
	case "nodes":
		return SlashCommand{Kind: CommandNodes, Arg: arg}, true
	case "memory":
		return SlashCommand{Kind: CommandMemory, Arg: arg}, true
	case "resume":
		return SlashCommand{Kind: CommandResume, Arg: arg}, true
	case "thinking":
		return SlashCommand{Kind: CommandThinking, Arg: arg}, true
	case "agent":
		return SlashCommand{Kind: CommandAgent, Arg: arg}, true
	case "subagents", "agents":
		return SlashCommand{Kind: CommandSubagents, Arg: arg}, true
	default:
		if arg != "" {
			return SlashCommand{Kind: CommandUnknown, Arg: name + " " + arg}, true
		}
		return SlashCommand{Kind: CommandUnknown, Arg: name}, true
	}
}
