package domain

import (
	"regexp"
	"slices"
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

// ProviderMode says how a provider's models are kept: the ones an admin
// picked, or every model the provider lists, synced every 6 hours.
type ProviderMode string

const (
	ModePick ProviderMode = "pick"
	ModeSync ProviderMode = "sync"
)

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

var (
	aliasPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
	providerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)
)

// ValidAlias says whether s may be an agent's alias, the model name sent
// to LiteLLM.
func ValidAlias(s string) bool { return aliasPattern.MatchString(s) }

// ValidProviderID says whether s may be a provider's id, its LiteLLM
// credential name.
func ValidProviderID(s string) bool { return providerPattern.MatchString(s) }

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
		"LiteLLM cannot be reached; the settings stay as they were")
	ErrGatewayRefused = apperr.New(apperr.KindInvalid, "ai_gateway_refused",
		"LiteLLM refused the change; the settings stay as they were")
	ErrProviderNotFound = apperr.New(apperr.KindNotFound, "ai_provider_not_found", "no such provider")
	ErrProviderExists   = apperr.New(apperr.KindConflict, "ai_provider_exists", "a provider has this id already")
	ErrProviderInvalid  = apperr.New(apperr.KindInvalid, "ai_provider_invalid",
		"the id is 2 to 41 lower-case letters, digits or dashes, and the kind is LiteLLM's provider route")
	ErrProviderInUse = apperr.New(apperr.KindConflict, "ai_provider_in_use",
		"an agent's model has no other provider; pick another model for the agent first")
	ErrModelNotFound  = apperr.New(apperr.KindNotFound, "ai_model_not_found", "no provider has this model")
	ErrModelLacksTags = apperr.New(apperr.KindInvalid, "ai_model_lacks_tags",
		"the model lacks a tag the agent requires")
	ErrTagUnknown    = apperr.New(apperr.KindInvalid, "ai_tag_unknown", "no such tag")
	ErrAgentNotFound = apperr.New(apperr.KindNotFound, "ai_agent_not_found",
		"no such agent, or not a console agent a member may pick")
	ErrMemberNotFound = apperr.New(apperr.KindNotFound, "member_not_found", "no such console member")
	ErrAgentDisabled  = apperr.New(apperr.KindConflict, "ai_agent_disabled", "the agent is turned off")
	ErrAgentInvalid   = apperr.New(apperr.KindInvalid, "ai_agent_invalid",
		"the agent's name, alias, tools or limits do not fit")
	ErrAgentTaken = apperr.New(apperr.KindConflict, "ai_agent_taken", "another agent has this name or alias")
	// ErrAppAgentFixed answers a change to an app agent other than its
	// model, limits and budget.
	ErrAppAgentFixed = apperr.New(apperr.KindInvalid, "ai_agent_app_fixed",
		"an app agent's model, limits and budget may change; the rest is the server's code")
)

var kindPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,30}$`)

// ValidProviderKind says whether s may be a provider's kind, LiteLLM's
// provider route such as openai, anthropic or openrouter.
func ValidProviderKind(s string) bool { return kindPattern.MatchString(s) }
