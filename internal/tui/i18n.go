package tui

import "strings"

type uiLanguage string

const (
	uiLanguageEnglish uiLanguage = "en-US"
	uiLanguageChinese uiLanguage = "zh-CN"
)

func normalizeUILanguage(value string) uiLanguage {
	if lang, ok := parseUILanguage(value); ok {
		return lang
	}
	return uiLanguageEnglish
}

func parseUILanguage(value string) (uiLanguage, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "zh", "zh-cn", "zh_cn", "cn", "chinese", "中文":
		return uiLanguageChinese, true
	case "en", "en-us", "en_us", "english":
		return uiLanguageEnglish, true
	default:
		return "", false
	}
}

func (l uiLanguage) tr(en, zh string) string {
	if l == uiLanguageChinese {
		return zh
	}
	return en
}

func (l uiLanguage) commandDescription(name string) string {
	if l != uiLanguageChinese {
		return commandDescriptionEnglish(name)
	}
	switch name {
	case "help":
		return "显示帮助"
	case "clear":
		return "清空对话"
	case "compact":
		return "压缩对话上下文"
	case "exit":
		return "退出 Conan"
	case "cluster":
		return "切换/显示集群"
	case "lang":
		return "切换界面语言"
	case "model":
		return "切换/显示模型"
	case "nodes":
		return "打开节点选择器"
	case "memory":
		return "查看记忆摘要"
	case "resume":
		return "恢复会话"
	case "thinking":
		return "为下一条消息开启 thinking"
	case "agent":
		return "运行本地 subagent"
	case "subagents":
		return "管理本地 subagents"
	default:
		return commandDescriptionEnglish(name)
	}
}

func commandDescriptionEnglish(name string) string {
	for _, cmd := range commandRegistry {
		if cmd.Name == name {
			return cmd.Description
		}
	}
	return ""
}

func (l uiLanguage) configValue() string {
	if l == uiLanguageChinese {
		return string(uiLanguageChinese)
	}
	return string(uiLanguageEnglish)
}

func (l uiLanguage) displayName() string {
	if l == uiLanguageChinese {
		return "中文"
	}
	return "English"
}
