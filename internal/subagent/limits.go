package subagent

// stubRoleLimits mirrors the user's WIP configschema.SubagentRoleLimits
// (pkg/configschema/config.go). Task 2 will replace this with an import.
type stubRoleLimits struct {
	InvestigatorTurns     int
	InvestigatorToolCalls int
	ReviewerTurns         int
	ReviewerToolCalls     int
	SummarizerTurns       int
}

type RoleLimits struct {
	InvestigatorTurns    int
	InvestigatorToolCalls int
	ReviewerTurns        int
	ReviewerToolCalls    int
	SummarizerTurns      int
}

const (
	defaultInvestigatorTurns    = 8
	defaultInvestigatorToolCalls = 12
	defaultReviewerTurns        = 4
	defaultReviewerToolCalls    = 6
	defaultSummarizerTurns      = 2
)

func NormalizeRoleLimits(cfg stubRoleLimits) RoleLimits {
	r := RoleLimits{
		InvestigatorTurns:    cfg.InvestigatorTurns,
		InvestigatorToolCalls: cfg.InvestigatorToolCalls,
		ReviewerTurns:        cfg.ReviewerTurns,
		ReviewerToolCalls:    cfg.ReviewerToolCalls,
		SummarizerTurns:      cfg.SummarizerTurns,
	}
	if r.InvestigatorTurns <= 0 {
		r.InvestigatorTurns = defaultInvestigatorTurns
	}
	if r.InvestigatorToolCalls <= 0 {
		r.InvestigatorToolCalls = defaultInvestigatorToolCalls
	}
	if r.ReviewerTurns <= 0 {
		r.ReviewerTurns = defaultReviewerTurns
	}
	if r.ReviewerToolCalls <= 0 {
		r.ReviewerToolCalls = defaultReviewerToolCalls
	}
	if r.SummarizerTurns <= 0 {
		r.SummarizerTurns = defaultSummarizerTurns
	}
	return r
}

func (r RoleLimits) For(role Role) (turns, toolCalls int) {
	switch normalizeRole(role) {
	case RoleReviewer:
		return r.ReviewerTurns, r.ReviewerToolCalls
	case RoleSummarizer:
		return r.SummarizerTurns, 0
	default:
		return r.InvestigatorTurns, r.InvestigatorToolCalls
	}
}
