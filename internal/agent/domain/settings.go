package domain

import (
	"regexp"
	"slices"
	"strings"
	"uuid"

	"github.com/shopnexus/server/internal/platform/apperr"
)

// AgentKind says who calls an agent: the server's code (app) or the agent
// service for a console member (console).
type AgentKind string

const (
	AgentApp     AgentKind = "app"
	AgentConsole AgentKind = "console"
)

// ConsoleAssistant is the console agent a turn runs when it names none,
// seeded by migration 0056.
var ConsoleAssistant = uuid.MustParse("0193e000-0000-7000-8000-00000000a101")

// TagKind is a tag's kind: a model's type or something it can do.
type TagKind string

// TagEmbedding is the type of an embedding model. An agent that requires it
// takes no fallback: another model's vectors are in another space.
const TagEmbedding = "Embedding"

// ModelRef is a provider's model, which callers send as provider/model.
type ModelRef struct {
	Provider string `json:"provider"`
	Model    string `json:"model_id"`
}

func (m ModelRef) String() string { return m.Provider + "/" + m.Model }

// ParseModelRef reads provider/model; the model may hold more slashes.
func ParseModelRef(s string) (ModelRef, bool) {
	provider, model, ok := strings.Cut(s, "/")
	if !ok || provider == "" || model == "" {
		return ModelRef{}, false
	}
	return ModelRef{Provider: provider, Model: model}, true
}

// Limits are an agent's limits. A console agent's go to the agent service
// in agent_config.limits: the turn's deadline, the model calls a segment
// may make, the context window, each tool call's time limit and attempts,
// and the tokens of an answer. An app agent's TimeoutS is its call's time
// limit. A zero field is unset and takes the agent service's default.
type Limits struct {
	DeadlineS     int `json:"deadline_s,omitempty"`
	MaxModelCalls int `json:"max_model_calls,omitempty"`
	ContextWindow int `json:"context_window,omitempty"`
	ToolTimeoutS  int `json:"tool_timeout_s,omitempty"`
	ToolAttempts  int `json:"tool_attempts,omitempty"`
	MaxTokens     int `json:"max_tokens,omitempty"`
	TimeoutS      int `json:"timeout_s,omitempty"`
}

// A console agent's deadline is 60 s by default, from 10 s up to 300 s for
// synthesis agents.
const (
	DefaultDeadlineS = 60
	MinDeadlineS     = 10
	MaxDeadlineS     = 300
)

// Check refuses limits outside their bounds, and fills a console agent's
// deadline when it has none.
func (l Limits) Check(kind AgentKind) (Limits, error) {
	if kind == AgentConsole && l.DeadlineS == 0 {
		l.DeadlineS = DefaultDeadlineS
	}
	if (l.DeadlineS != 0 && (l.DeadlineS < MinDeadlineS || l.DeadlineS > MaxDeadlineS)) || l.MaxModelCalls < 0 ||
		l.ContextWindow < 0 || l.ToolTimeoutS < 0 || l.ToolAttempts < 0 || l.MaxTokens < 0 || l.TimeoutS < 0 {
		return l, ErrAgentInvalid.WithField("limits", apperr.OutOfRange, "out_of_range")
	}
	return l, nil
}

var agentSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

// ValidAgentSlug says whether s may be an agent's slug, its team's name in
// Bifrost and a console agent's caller.
func ValidAgentSlug(s string) bool { return agentSlugPattern.MatchString(s) }

// MissingTags is the tags of required that have lacks, in required's
// order.
func MissingTags(required, has []string) []string {
	var out []string
	for _, t := range required {
		if !slices.Contains(has, t) {
			out = append(out, t)
		}
	}
	return out
}

var (
	ErrGatewayUnavailable = apperr.New(apperr.KindUnavailable, "ai_gateway_unavailable",
		"Bifrost cannot be reached; a saved change goes to Bifrost when it is back")
	ErrGatewayRefused = apperr.New(apperr.KindInvalid, "ai_gateway_refused",
		"Bifrost refused the change")
	ErrProviderNotFound = apperr.New(apperr.KindNotFound, "ai_provider_not_found", "Bifrost runs no such provider")
	ErrModelUnpriced    = apperr.New(apperr.KindInvalid, "ai_model_unpriced",
		"the model has no price; set its price on the models page first")
	ErrModelLacksTags = apperr.New(apperr.KindInvalid, "ai_model_lacks_tags",
		"the model lacks a tag the agent requires")
	ErrTagUnknown    = apperr.New(apperr.KindInvalid, "ai_tag_unknown", "no such tag")
	ErrAgentNotFound = apperr.New(apperr.KindNotFound, "ai_agent_not_found",
		"no such agent, or not a console agent a member may pick")
	ErrAgentDisabled = apperr.New(apperr.KindConflict, "ai_agent_disabled", "the agent is turned off")
	ErrAgentInvalid  = apperr.New(apperr.KindInvalid, "ai_agent_invalid",
		"the agent's name, slug, fallbacks, callers, budget, tools or limits do not fit")
	ErrAgentTaken = apperr.New(apperr.KindConflict, "ai_agent_taken", "another agent or caller has this name or slug")
	// ErrAppAgentFixed answers a change to an app agent other than its
	// model, fallbacks, limits, budget and callers' rate limits.
	ErrAppAgentFixed = apperr.New(apperr.KindInvalid, "ai_agent_app_fixed",
		"an app agent's model, fallbacks, limits, budget and rate limits may change; the rest is the server's code")
	ErrPriceInvalid = apperr.New(apperr.KindInvalid, "ai_price_invalid",
		"a price is a number of USD per token, 0 or more")
	ErrSpendRange = apperr.New(apperr.KindInvalid, "ai_spend_range_invalid",
		"from is on or before to, at most 366 days apart")
)
