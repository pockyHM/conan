package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	cfgloader "github.com/pockyHM/conan/internal/config"
	"github.com/pockyHM/conan/pkg/configschema"
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
	renderChoice(p.out, prompt, options, selected)
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
			fmt.Fprintln(p.out)
			return selected, nil
		case n >= 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'A':
			if selected > 0 {
				selected--
				moveChoiceCursorUp(p.out, len(options))
				renderChoice(p.out, prompt, options, selected)
			}
		case n >= 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'B':
			if selected < len(options)-1 {
				selected++
				moveChoiceCursorUp(p.out, len(options))
				renderChoice(p.out, prompt, options, selected)
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

func runModelAdd(in io.Reader, out io.Writer, loader *cfgloader.Loader, lister ModelLister) error {
	pr := newPrompter(in, out)

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

	configName, err := pr.ask("Config name: ")
	if err != nil {
		return err
	}
	if configName == "" {
		return fmt.Errorf("config name is required")
	}

	global, err := loader.LoadGlobal()
	if err != nil {
		return err
	}
	for _, m := range global.Models {
		if m.Name == configName {
			return fmt.Errorf("model %q already exists", configName)
		}
	}

	apiKey, err := pr.askSecret("API key: ")
	if err != nil {
		return err
	}

	endpoint := preset.Endpoint
	if preset.NeedsEndpoint {
		endpoint, err = pr.ask("Endpoint: ")
		if err != nil {
			return err
		}
		if endpoint == "" {
			return fmt.Errorf("endpoint is required for custom providers")
		}
	}

	modelID, err := selectModel(pr, lister, preset, endpoint, apiKey)
	if err != nil {
		return err
	}

	setDefault := false
	if len(global.Models) == 0 {
		setDefault = true
	} else {
		setDefault, err = pr.confirm("Set as default model?")
		if err != nil {
			return err
		}
	}

	global.Models = append(global.Models, configschema.ModelConfig{
		Name:                configName,
		Type:                preset.Type,
		Endpoint:            endpoint,
		UseEndpointDirectly: preset.UseEndpointDirectly,
		Model:               modelID,
		APIKey:              apiKey,
	})
	if setDefault {
		global.DefaultModel = configName
	}

	if err := loader.SaveGlobal(global); err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved model %s to %s\n", configName, loader.ConfigPath())
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

	options := append(models, "Enter manually")
	idx, err := pr.choose("Select model", options)
	if err != nil {
		return "", err
	}
	if idx == len(models) {
		return askModelName(pr, preset.DefaultModelHint)
	}
	return models[idx], nil
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
