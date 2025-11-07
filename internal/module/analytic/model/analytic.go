package analyticmodel

import (
	"encoding/json"
	"time"

	"shopnexus-remastered/internal/db"
)

const (
	TopicAnalyticInteraction = "analytic.interaction"
)

type Interaction struct {
	ID          int64                         `json:"id"`
	AccountID   int64                         `json:"account_id"`
	EventType   string                        `json:"event_type"`
	RefType     db.AnalyticInteractionRefType `json:"ref_type"`
	RefID       int64                         `json:"ref_id"`
	Metadata    json.RawMessage               `json:"metadata"`
	DateCreated time.Time                     `json:"date_created"`
}
