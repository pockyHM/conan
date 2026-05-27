package main

type ModelPreset struct {
	ID                  string
	DisplayName         string
	Type                string
	Endpoint            string
	EnvKey              string
	SupportsList        bool
	NeedsEndpoint       bool
	NeedsType           bool
	UseEndpointDirectly bool
	DefaultModelHint    string
	RecommendedModels   []string
}

var modelPresets = []ModelPreset{
	{ID: "anthropic", DisplayName: "Anthropic", Type: "anthropic", Endpoint: "https://api.anthropic.com", EnvKey: "ANTHROPIC_API_KEY", DefaultModelHint: "claude-sonnet-4-6", RecommendedModels: []string{"claude-sonnet-4-6", "claude-opus-4-6"}},
	{ID: "openai", DisplayName: "OpenAI", Type: "openai", Endpoint: "https://api.openai.com/v1", EnvKey: "OPENAI_API_KEY", SupportsList: true, DefaultModelHint: "gpt-4.1", RecommendedModels: []string{"gpt-4.1", "o3"}},
	{ID: "glm", DisplayName: "GLM", Type: "openai", Endpoint: "https://open.bigmodel.cn/api/paas/v4", EnvKey: "GLM_API_KEY", SupportsList: true, DefaultModelHint: "glm-4.5"},
	{ID: "glm-coding", DisplayName: "GLM Coding Plan", Type: "openai", Endpoint: "https://open.bigmodel.cn/api/coding/paas/v4", EnvKey: "GLM_API_KEY", SupportsList: true, DefaultModelHint: "glm-4.5"},
	{ID: "minimax", DisplayName: "MiniMax (International)", Type: "openai", Endpoint: "https://api.minimax.io/v1", EnvKey: "MINIMAX_API_KEY", SupportsList: true, DefaultModelHint: "MiniMax-M1"},
	{ID: "minimax-cn", DisplayName: "MiniMax (China)", Type: "openai", Endpoint: "https://api.minimaxi.com/v1", EnvKey: "MINIMAX_API_KEY", SupportsList: true, DefaultModelHint: "MiniMax-M1"},
	{ID: "qwen", DisplayName: "Qwen", Type: "openai", Endpoint: "https://dashscope.aliyuncs.com/compatible-mode/v1", EnvKey: "DASHSCOPE_API_KEY", SupportsList: true, DefaultModelHint: "qwen-max"},
	{ID: "kimi", DisplayName: "Kimi", Type: "openai", Endpoint: "https://api.moonshot.cn/v1", EnvKey: "MOONSHOT_API_KEY", SupportsList: true, DefaultModelHint: "kimi-k2"},
	{ID: "custom", DisplayName: "Custom", NeedsEndpoint: true, NeedsType: true},
}

func modelPresetByID(id string) (ModelPreset, bool) {
	for _, preset := range modelPresets {
		if preset.ID == id {
			return preset, true
		}
	}
	return ModelPreset{}, false
}
