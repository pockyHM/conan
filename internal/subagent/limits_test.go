package subagent

import (
	"testing"

	"github.com/pockyHM/conan/pkg/configschema"
)

func TestNormalizeRoleLimitsAppliesDefaults(t *testing.T) {
	got := NormalizeRoleLimits(configschema.SubagentRoleLimits{})

	cases := []struct {
		role  Role
		turns int
		calls int
	}{
		{RoleInvestigator, 8, 12},
		{RoleReviewer, 4, 6},
		{RoleSummarizer, 2, 0},
	}
	for _, c := range cases {
		turns, calls := got.For(c.role)
		if turns != c.turns || calls != c.calls {
			t.Errorf("For(%s) = (%d, %d), want (%d, %d)", c.role, turns, calls, c.turns, c.calls)
		}
	}
}

func TestNormalizeRoleLimitsPreservesCustomValues(t *testing.T) {
	cfg := configschema.SubagentRoleLimits{
		InvestigatorTurns:    16,
		InvestigatorToolCalls: 20,
		ReviewerTurns:        2,
		ReviewerToolCalls:    4,
		SummarizerTurns:      1,
	}
	got := NormalizeRoleLimits(cfg)

	turns, calls := got.For(RoleInvestigator)
	if turns != 16 || calls != 20 {
		t.Errorf("investigator = (%d, %d), want (16, 20)", turns, calls)
	}
	turns, _ = got.For(RoleReviewer)
	if turns != 2 {
		t.Errorf("reviewer turns = %d, want 2", turns)
	}
	turns, _ = got.For(RoleSummarizer)
	if turns != 1 {
		t.Errorf("summarizer turns = %d, want 1", turns)
	}
}

func TestRoleLimitsFallsBackToDefaultsForZeroValues(t *testing.T) {
	got := NormalizeRoleLimits(configschema.SubagentRoleLimits{
		InvestigatorTurns: 0,
	})

	turns, calls := got.For(RoleInvestigator)
	if turns != 8 || calls != 12 {
		t.Errorf("zero turns must fall back to defaults; got (%d, %d)", turns, calls)
	}
}
