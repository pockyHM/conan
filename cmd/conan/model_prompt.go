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
	for i, opt := range options {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, opt)
	}
	fmt.Fprintf(p.out, "%s [1-%d]: ", prompt, len(options))
	line, err := p.in.ReadString('\n')
	if err != nil && err != io.EOF {
		return 0, err
	}
	line = strings.TrimSpace(line)
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
		Name:     configName,
		Type:     preset.Type,
		Endpoint: endpoint,
		Model:    modelID,
		APIKey:   apiKey,
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
		return pr.ask(fmt.Sprintf("Model [%s]: ", preset.DefaultModelHint))
	}

	fmt.Fprint(pr.out, "Fetching available models...\n")
	ctx := context.Background()
	models, err := lister.ListModels(ctx, endpoint, apiKey)
	if err != nil {
		fmt.Fprintf(pr.out, "Warning: could not fetch models (%v)\n", err)
		return pr.ask(fmt.Sprintf("Model [%s]: ", preset.DefaultModelHint))
	}
	if len(models) == 0 {
		return pr.ask(fmt.Sprintf("Model [%s]: ", preset.DefaultModelHint))
	}

	options := append(models, "Enter manually")
	idx, err := pr.choose("Select model", options)
	if err != nil {
		return "", err
	}
	if idx == len(models) {
		return pr.ask(fmt.Sprintf("Model [%s]: ", preset.DefaultModelHint))
	}
	return models[idx], nil
}
