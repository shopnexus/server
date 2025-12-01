package analyticmodel

import (
	"encoding/json"
	analyticdb "shopnexus-remastered/internal/module/analytic/db"
	"time"

	"github.com/google/uuid"
)

const (
	TopicAnalyticInteraction = "analytic.interaction"
)

type Interaction struct {
	ID          int64                                 `json:"id"`
	AccountID   uuid.NullUUID                         `json:"account_id"`
	EventType   string                                `json:"event_type"`
	RefType     analyticdb.AnalyticInteractionRefType `json:"ref_type"`
	RefID       string                                `json:"ref_id"`
	Metadata    json.RawMessage                       `json:"metadata"`
	DateCreated time.Time                             `json:"date_created"`
}
