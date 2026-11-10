package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/trust/domain"
)

func TestNewReport(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	reporter, seller, listing := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	r, err := domain.NewReport(uuid.NewV7(), reporter, seller, listing, uuid.UUID{}, domain.ReasonFake, "  Ảnh lấy trên mạng  ", at)
	if err != nil || r.Note() != "Ảnh lấy trên mạng" || r.AccountID() != seller || r.ListingID() != listing ||
		r.Reason() != domain.ReasonFake || !r.CreatedAt().Equal(at) || !r.UpdatedAt().Equal(at) || r.WithdrawnAt() != nil {
		t.Fatalf("%+v %v", r.State(), err)
	}
	if _, err = domain.NewReport(uuid.NewV7(), reporter, reporter, uuid.UUID{}, uuid.UUID{}, domain.ReasonScam, "", at); !errors.Is(err, domain.ErrSelfReport) {
		t.Fatalf("self: %v", err)
	}
	_, err = domain.NewReport(uuid.NewV7(), reporter, seller, uuid.UUID{}, uuid.UUID{}, "ugly", strings.Repeat("ạ", domain.MaxNote+1), at)
	if e, ok := errors.AsType[*apperr.Error](err); !ok || e.Code != "invalid_report" || e.Fields["reason"] == "" || e.Fields["note"] == "" {
		t.Fatalf("broken rules: %v", err)
	}
	if _, err = domain.NewReport(uuid.NewV7(), reporter, seller, uuid.UUID{}, uuid.UUID{}, domain.ReasonOther, strings.Repeat("ạ", domain.MaxNote), at); err != nil {
		t.Fatalf("500 characters: %v", err)
	}
}

func TestNewReportConversation(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	reporter, seller, conversation := uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
	r, err := domain.NewReport(uuid.NewV7(), reporter, seller, uuid.UUID{}, conversation, domain.ReasonScam, "", at)
	if err != nil || r.ConversationID() != conversation || r.State().ConversationID != conversation {
		t.Fatalf("%+v %v", r.State(), err)
	}
	_, err = domain.NewReport(uuid.NewV7(), reporter, seller, uuid.NewV7(), conversation, domain.ReasonScam, "", at)
	if e, ok := errors.AsType[*apperr.Error](err); !ok || e.Code != "invalid_report" || e.Fields["conversation_id"] == "" {
		t.Fatalf("with a listing: %v", err)
	}
}
