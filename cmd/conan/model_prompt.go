package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/configschema"
	"github.com/pockyHM/conan/pkg/models"
	"golang.org/x/term"
)

type prompter struct {
	in    *bufio.Reader
	rawIn io.Reader
	out   io.Writer
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	return &prompter{in: bufio.NewReader(in), rawIn: in, out: out}
}

func (p *prompter) ask(prompt string) (string, error) {
	fmt.Fprint(p.out, prompt)
	line, err := p.in.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (p *prompter) askSecret(prompt string) (string, error) {
	fmt.Fprint(p.out, prompt)
	if f, ok := p.rawIn.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(p.out)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return p.ask("")
}

func (p *prompter) choose(prompt string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, fmt.Errorf("no options available")
	}

	if f, ok := p.rawIn.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return p.chooseInteractive(f, prompt, options)
	}

	renderChoice(p.out, prompt, options, 0)
	line, err := p.in.ReadString('\n')
	if err != nil && err != io.EOF {
		return 0, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return 0, nil
	}
	var idx int
	for _, ch := range line {
		if ch >= '1' && ch <= '9' {
			idx = int(ch - '0')
			break
		}
	}
	if idx < 1 || idx > len(options) {
		return 0, fmt.Errorf("invalid selection: %q", line)
	}
	return idx - 1, nil
}

func (p *prompter) chooseInteractive(f *os.File, prompt string, options []string) (int, error) {
	oldState, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		return 0, err
	}
	defer term.Restore(int(f.Fd()), oldState)

	selected := 0
	renderChoiceTerminal(p.out, prompt, options, selected)
	buf := make([]byte, 3)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		switch {
		case buf[0] == '\r' || buf[0] == '\n':
			fmt.Fprint(p.out, "\r\n")
			return selected, nil
		case n >= 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'A':
			if selected > 0 {
				selected--
				moveChoiceCursorUp(p.out, len(options))
				renderChoiceTerminal(p.out, prompt, options, selected)
			}
		case n >= 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'B':
			if selected < len(options)-1 {
				selected++
				moveChoiceCursorUp(p.out, len(options))
				renderChoiceTerminal(p.out, prompt, options, selected)
			}
		}
	}
}

func renderChoice(out io.Writer, prompt string, options []string, selected int) {
	fmt.Fprintf(out, "%s\n", prompt)
	for i, opt := range options {
		cursor := "  "
		if i == selected {
			cursor = "> "
		}
		fmt.Fprintf(out, "%s%s\n", cursor, opt)
	}
}

func renderChoiceTerminal(out io.Writer, prompt string, options []string, selected int) {
	fmt.Fprintf(out, "\r\x1b[2K%s\r\n", prompt)
	for i, opt := range options {
		cursor := "  "
		if i == selected {
			cursor = "> "
		}
		fmt.Fprintf(out, "\r\x1b[2K%s%s\r\n", cursor, opt)
	}
}

func moveChoiceCursorUp(out io.Writer, optionCount int) {
	fmt.Fprintf(out, "\x1b[%dA", optionCount+1)
}

func (p *prompter) confirm(prompt string) (bool, error) {
	fmt.Fprintf(p.out, "%s [y/N]: ", prompt)
	line, err := p.in.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y"), nil
}

func (p *prompter) toggle(prompt string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, nil
	}
	if f, ok := p.rawIn.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return p.toggleInteractive(f, prompt, options)
	}
	return 0, nil
}

func (p *prompter) toggleInteractive(f *os.File, prompt string, options []string) (int, error) {
	oldState, err := term.MakeRaw(int(f.Fd()))
	if err != nil {
		return 0, err
	}
	defer term.Restore(int(f.Fd()), oldState)

	selected := 0
	renderToggle(p.out, prompt, options, selected)
	buf := make([]byte, 3)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		switch {
		case buf[0] == '\r' || buf[0] == '\n':
			fmt.Fprint(p.out, "\r\n")
			return selected, nil
		case n >= 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'D':
			if selected > 0 {
				selected--
				renderToggle(p.out, prompt, options, selected)
			}
		case n >= 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'C':
			if selected < len(options)-1 {
				selected++
				renderToggle(p.out, prompt, options, selected)
			}
		}
	}
}

func renderToggle(out io.Writer, prompt string, options []string, selected int) {
	fmt.Fprintf(out, "\r\x1b[2K%s: ", prompt)
	for i, opt := range options {
		if i == selected {
			fmt.Fprintf(out, "\x1b[1;36m[%s]\x1b[0m ", opt)
		} else {
			fmt.Fprintf(out, "%s ", opt)
		}
	}
	fmt.Fprint(out, "  \x1b[2m←/→\x1b[0m")
}

// ConnectionTester verifies that a model config can connect.
type ConnectionTester interface {
	TestConnection(ctx context.Context, cfg configschema.ModelConfig) error
}

type LiveConnectionTester struct{}

func (t LiveConnectionTester) TestConnection(ctx context.Context, cfg configschema.ModelConfig) error {
	provider, _, err := llm.NewProvider([]configschema.ModelConfig{cfg}, cfg.Name)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	_, err = provider.Chat(ctx, &llm.ChatRequest{
		Messages:  []models.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 1,
	})
	return err
}

func generateName(preset ModelPreset, loader *cfgloader.Loader) string {
	base := preset.ID
	if base == "" {
		base = "custom"
	}
	global, err := loader.LoadGlobal()
	if err != nil {
		return base
	}
	name := base
	for i := 2; modelExists(global.Models, name); i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

func maskKey(key string) string {
	if len(key) <= 4 {
		return "****"
	}
	return key[:2] + strings.Repeat("*", len(key)-4) + key[len(key)-2:]
}

// ModelAddFlags holds parsed CLI flags for non-interactive model add.
type ModelAddFlags struct {
	Provider     string
	APIKey       string
	Model        string
	Name         string
	Type         string
	Endpoint     string
	EndpointMode string
	SetDefault   bool
}

func runModelAdd(in io.Reader, out io.Writer, loader *cfgloader.Loader, lister ModelLister, tester ConnectionTester, flags ModelAddFlags) error {
	if flags.Provider != "" {
		return runModelAddDirect(out, loader, tester, flags)
	}
	return runModelAddInteractive(in, out, loader, lister, tester)
}

func runModelAddDirect(out io.Writer, loader *cfgloader.Loader, tester ConnectionTester, flags ModelAddFlags) error {
	preset, ok := modelPresetByID(strings.ToLower(flags.Provider))
	if !ok {
		return fmt.Errorf("unknown provider %q (available: anthropic, openai, glm, glm-coding, minimax, minimax-cn, qwen, kimi, custom)", flags.Provider)
	}

	if preset.NeedsType {
		if flags.Type == "" {
			return fmt.Errorf("--type is required for custom provider (openai or anthropic)")
		}
		switch strings.ToLower(flags.Type) {
		case "openai":
			preset.Type = "openai"
			preset.SupportsList = true
			preset.DefaultModelHint = "gpt-4.1"
		case "anthropic":
			preset.Type = "anthropic"
			preset.DefaultModelHint = "claude-sonnet-4-6"
		default:
			return fmt.Errorf("unknown type %q (openai or anthropic)", flags.Type)
		}
	}

	if flags.Name == "" {
		flags.Name = generateName(preset, loader)
	}

	apiKey := flags.APIKey
	if apiKey == "" && preset.EnvKey != "" {
		apiKey = os.Getenv(preset.EnvKey)
	}
	if apiKey == "" {
		return fmt.Errorf("--api-key is required (or set %s)", preset.EnvKey)
	}

	endpoint := preset.Endpoint
	useEndpointDirectly := preset.UseEndpointDirectly
	if preset.NeedsEndpoint {
		if flags.Endpoint == "" {
			return fmt.Errorf("--endpoint is required for custom provider")
		}
		endpoint = flags.Endpoint
		if flags.EndpointMode == "full" {
			useEndpointDirectly = true
		}
	}

	if flags.Model == "" {
		return fmt.Errorf("--model is required")
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		return err
	}
	if modelExists(global.Models, flags.Name) {
		return fmt.Errorf("model %q already exists", flags.Name)
	}

	cfg := configschema.ModelConfig{
		Name:                flags.Name,
		Type:                preset.Type,
		Endpoint:            endpoint,
		UseEndpointDirectly: useEndpointDirectly,
		Model:               flags.Model,
		APIKey:              apiKey,
	}

	fmt.Fprintf(out, "Testing connection...\n")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := tester.TestConnection(ctx, cfg); err != nil {
		fmt.Fprintf(out, "Warning: connection test failed: %v\n", err)
	}

	global.Models = append(global.Models, cfg)
	setDefault := flags.SetDefault || len(global.Models) == 1
	if setDefault {
		global.DefaultModel = flags.Name
	}
	if err := loader.SaveGlobal(global); err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved model %s (%s) to %s\n", flags.Name, flags.Model, loader.ConfigPath())
	return nil
}

func runModelAddInteractive(in io.Reader, out io.Writer, loader *cfgloader.Loader, lister ModelLister, tester ConnectionTester) error {
	pr := newPrompter(in, out)

	// Provider selection is outside the retry loop — user rarely changes this.
	names := make([]string, len(modelPresets))
	for i, p := range modelPresets {
		names[i] = p.DisplayName
	}
	idx, err := pr.choose("Provider", names)
	if err != nil {
		return err
	}
	preset := modelPresets[idx]

	if preset.NeedsType {
		idx, err := pr.choose("Compatibility", []string{"OpenAI-compatible", "Anthropic-compatible"})
		if err != nil {
			return err
		}
		switch idx {
		case 0:
			preset.Type = "openai"
			preset.SupportsList = true
			preset.DefaultModelHint = "gpt-4.1"
		case 1:
			preset.Type = "anthropic"
			preset.SupportsList = false
			preset.DefaultModelHint = "claude-sonnet-4-6"
		}
	}

	defaultName := generateName(preset, loader)

	var cfg configschema.ModelConfig
	var setDefault bool

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			fmt.Fprint(out, "\n--- Reconfigure ---\n")
		}

		// Config name
		namePrompt := fmt.Sprintf("Config name [%s]: ", defaultName)
		configName, err := pr.ask(namePrompt)
		if err != nil {
			return err
		}
		if configName == "" {
			configName = defaultName
		}

		global, err := loader.LoadGlobal()
		if err != nil {
			return err
		}
		if modelExists(global.Models, configName) {
			return fmt.Errorf("model %q already exists", configName)
		}

		// API key with env detection
		apiKey := ""
		if preset.EnvKey != "" {
			apiKey = os.Getenv(preset.EnvKey)
		}
		if apiKey != "" {
			fmt.Fprintf(out, "ℹ Detected %s in environment\n", preset.EnvKey)
			useEnv, err := pr.confirm(fmt.Sprintf("Use this key? (%s)", maskKey(apiKey)))
			if err != nil {
				return err
			}
			if !useEnv {
				apiKey, err = pr.askSecret("API key: ")
				if err != nil {
					return err
				}
			}
		} else {
			apiKey, err = pr.askSecret("API key: ")
			if err != nil {
				return err
			}
		}
		if apiKey == "" {
			return fmt.Errorf("API key is required")
		}

		// Endpoint (custom only)
		endpoint := preset.Endpoint
		useEndpointDirectly := preset.UseEndpointDirectly
		if preset.NeedsEndpoint {
			idx, err := pr.toggle("Endpoint mode", []string{"Base URL", "Full URL"})
			if err != nil {
				return err
			}
			useEndpointDirectly = idx == 1

			hint := "https://api.example.com/v1"
			if useEndpointDirectly {
				hint = "https://api.example.com/v1/chat/completions"
			}
			endpoint, err = pr.ask(fmt.Sprintf("Endpoint [%s]: ", hint))
			if err != nil {
				return err
			}
			if endpoint == "" {
				endpoint = hint
			}
		}

		// Model selection
		modelID, err := selectModel(pr, lister, preset, endpoint, apiKey)
		if err != nil {
			return err
		}

		cfg = configschema.ModelConfig{
			Name:                configName,
			Type:                preset.Type,
			Endpoint:            endpoint,
			UseEndpointDirectly: useEndpointDirectly,
			Model:               modelID,
			APIKey:              apiKey,
		}

		// Connection test
		fmt.Fprint(out, "Testing connection...\n")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		testErr := tester.TestConnection(ctx, cfg)
		cancel()

		if testErr == nil {
			fmt.Fprint(out, "✓ Connection test passed\n")
			break
		}

		fmt.Fprintf(out, "✗ Connection test failed: %v\n", testErr)
		modify, err := pr.confirm("Modify settings?")
		if err != nil {
			return err
		}
		if !modify {
			break
		}
		defaultName = configName
	}

	// Set as default
	global, err := loader.LoadGlobal()
	if err != nil {
		return err
	}
	if len(global.Models) == 0 {
		setDefault = true
	} else {
		setDefault, err = pr.confirm("Set as default model?")
		if err != nil {
			return err
		}
	}

	global.Models = append(global.Models, cfg)
	if setDefault {
		global.DefaultModel = cfg.Name
	}

	if err := loader.SaveGlobal(global); err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved model %s (%s) to %s\n", cfg.Name, cfg.Model, loader.ConfigPath())
	return nil
}

func selectModel(pr *prompter, lister ModelLister, preset ModelPreset, endpoint, apiKey string) (string, error) {
	if !preset.SupportsList {
		return askModelName(pr, preset.DefaultModelHint)
	}

	fmt.Fprint(pr.out, "Fetching available models...\n")
	ctx := context.Background()
	models, err := lister.ListModels(ctx, endpoint, apiKey)
	if err != nil {
		fmt.Fprintf(pr.out, "Warning: could not fetch models (%v)\n", err)
		return askModelName(pr, preset.DefaultModelHint)
	}
	if len(models) == 0 {
		return askModelName(pr, preset.DefaultModelHint)
	}

	options := make([]string, len(models))
	for i, m := range models {
		options[i] = m
		if isRecommended(m, preset.RecommendedModels) {
			options[i] = m + " (recommended)"
		}
	}
	options = append(options, "Enter manually")

	idx, err := pr.choose("Select model", options)
	if err != nil {
		return "", err
	}
	if idx == len(models) {
		return askModelName(pr, preset.DefaultModelHint)
	}
	return models[idx], nil
}

func isRecommended(model string, recommended []string) bool {
	for _, r := range recommended {
		if model == r {
			return true
		}
	}
	return false
}

func askModelName(pr *prompter, defaultHint string) (string, error) {
	prompt := "Model: "
	if defaultHint != "" {
		prompt = fmt.Sprintf("Model [%s]: ", defaultHint)
	}
	model, err := pr.ask(prompt)
	if err != nil {
		return "", err
	}
	if model == "" {
		model = defaultHint
	}
	if model == "" {
		return "", fmt.Errorf("model is required")
	}
	return model, nil
}
