package analyticmodel

import (
	"time"

	"shopnexus-remastered/internal/db"
)

const (
	TopicAnalyticInteraction = "analytic.interaction"
)

type Interaction struct {
	ID          string                        `json:"id"`
	AccountID   int64                         `json:"account_id"`
	EventType   string                        `json:"event_type"`
	RefType     db.AnalyticInteractionRefType `json:"ref_type"`
	RefID       int64                         `json:"ref_id"`
	Metadata    map[string]any                `json:"metadata,omitempty"`
	DateCreated time.Time                     `json:"date_created"`
}
