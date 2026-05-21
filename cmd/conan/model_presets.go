package main

type ModelPreset struct {
	ID               string
	DisplayName      string
	Type             string
	Endpoint         string
	SupportsList     bool
	NeedsEndpoint    bool
	DefaultModelHint string
}

var modelPresets = []ModelPreset{
	{ID: "anthropic", DisplayName: "Anthropic", Type: "anthropic", Endpoint: "https://api.anthropic.com", DefaultModelHint: "claude-sonnet-4-6"},
	{ID: "openai", DisplayName: "OpenAI", Type: "openai", Endpoint: "https://api.openai.com/v1", SupportsList: true, DefaultModelHint: "gpt-4.1"},
	{ID: "glm", DisplayName: "GLM", Type: "openai", Endpoint: "https://open.bigmodel.cn/api/paas/v4", SupportsList: true, DefaultModelHint: "glm-4.5"},
	{ID: "minimax", DisplayName: "MiniMax", Type: "openai", Endpoint: "https://api.minimax.chat/v1", SupportsList: true, DefaultModelHint: "MiniMax-M1"},
	{ID: "qwen", DisplayName: "Qwen", Type: "openai", Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", SupportsList: true, DefaultModelHint: "qwen-max"},
	{ID: "kimi", DisplayName: "Kimi", Type: "openai", Endpoint: "https://api.moonshot.cn/v1", SupportsList: true, DefaultModelHint: "kimi-k2"},
	{ID: "custom", DisplayName: "Custom OpenAI-compatible", Type: "openai", NeedsEndpoint: true, SupportsList: true},
}

func modelPresetByID(id string) (ModelPreset, bool) {
	for _, preset := range modelPresets {
		if preset.ID == id {
			return preset, true
		}
	}
	return ModelPreset{}, false
}
