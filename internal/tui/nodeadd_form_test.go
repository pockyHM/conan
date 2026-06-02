package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNodeAddFormDefaultsAndMasksPassword(t *testing.T) {
	form := newNodeAddForm(uiLanguageEnglish)
	form = form.withValue(nodeAddFormFieldName, "web-1")
	form = form.withValue(nodeAddFormFieldHost, "10.0.0.12")
	form = form.withValue(nodeAddFormFieldUser, "deploy")
	form = form.withValue(nodeAddFormFieldPassword, "secret")

	view := form.View()
	for _, want := range []string{"Add New Node", "Name", "Host/IP", "Agent port", "9280", "deploy"} {
		if !strings.Contains(view, want) {
			t.Fatalf("form view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "secret") {
		t.Fatalf("form view leaked password:\n%s", view)
	}
	if !strings.Contains(view, "******") {
		t.Fatalf("form view missing masked password:\n%s", view)
	}
}

func TestNodeAddFormInputAndAdvance(t *testing.T) {
	form := newNodeAddForm(uiLanguageEnglish)

	form, submitted := form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("web-1")})
	if submitted {
		t.Fatal("typing name submitted form")
	}
	form, submitted = form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if submitted {
		t.Fatal("enter on name submitted form")
	}
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("10.0.0.12")})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyTab})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9281")})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("deploy")})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyEnter})
	form, _ = form.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("secret")})
	form, submitted = form.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !submitted {
		t.Fatal("enter on password should submit form")
	}
	values, err := form.Values()
	if err != nil {
		t.Fatalf("Values returned error: %v", err)
	}
	if values.Name != "web-1" || values.Host != "10.0.0.12" || values.AgentPort != 9281 || values.User != "deploy" || values.Password != "secret" {
		t.Fatalf("values = %#v", values)
	}
}

func TestNodeAddFormValidation(t *testing.T) {
	form := newNodeAddForm(uiLanguageEnglish)
	if _, err := form.Values(); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("empty values err = %v, want name is required", err)
	}

	form = form.withValue(nodeAddFormFieldName, "web-1").
		withValue(nodeAddFormFieldHost, "10.0.0.12").
		withValue(nodeAddFormFieldPort, "bad").
		withValue(nodeAddFormFieldUser, "deploy").
		withValue(nodeAddFormFieldPassword, "secret")

	if _, err := form.Values(); err == nil || !strings.Contains(err.Error(), "agent port must be a number") {
		t.Fatalf("bad port err = %v, want agent port validation", err)
	}
}

func TestNodeAddFormAllowsBatchHostsWithMatchingNames(t *testing.T) {
	form := newNodeAddForm(uiLanguageEnglish).
		withValue(nodeAddFormFieldName, "web-1,web-2").
		withValue(nodeAddFormFieldHost, "10.0.0.12,10.0.0.13").
		withValue(nodeAddFormFieldUser, "deploy").
		withValue(nodeAddFormFieldPassword, "secret")

	values, err := form.Values()
	if err != nil {
		t.Fatalf("Values returned error: %v", err)
	}
	if len(values.Hosts) != 2 || values.Hosts[0] != "10.0.0.12" || values.Hosts[1] != "10.0.0.13" {
		t.Fatalf("hosts = %#v", values.Hosts)
	}
	if len(values.Names) != 2 || values.Names[0] != "web-1" || values.Names[1] != "web-2" {
		t.Fatalf("names = %#v", values.Names)
	}
}

func TestNodeAddFormRejectsBatchHostsWithMismatchedNames(t *testing.T) {
	form := newNodeAddForm(uiLanguageEnglish).
		withValue(nodeAddFormFieldName, "web-1").
		withValue(nodeAddFormFieldHost, "10.0.0.12,10.0.0.13").
		withValue(nodeAddFormFieldUser, "deploy").
		withValue(nodeAddFormFieldPassword, "secret")

	if _, err := form.Values(); err == nil || !strings.Contains(err.Error(), "name must be empty or contain 2 comma-separated values") {
		t.Fatalf("err = %v", err)
	}
}
