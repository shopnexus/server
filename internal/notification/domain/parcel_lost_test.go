package domain_test

import (
	"testing"
	"uuid"

	"github.com/shopnexus/server/internal/notification/domain"
	"github.com/shopnexus/server/internal/platform/locale"
)

// A parcel the carrier lost tells the seller to claim with the carrier and
// the buyer that they pay nothing, in place of a failed delivery.
//
// UC-DEAL-05 E4.
func TestParcelLost(t *testing.T) {
	t.Parallel()
	order := uuid.NewV7()
	got := domain.FromMoves(facts(domain.Move{Kind: "failed", By: "system", OrderID: order, Carrier: "ghn", TrackingCode: "G1", Lost: true}), at)
	if len(got) != 2 || got[0].Recipient != seller || got[1].Recipient != buyer ||
		got[0].Type != domain.TypeParcelLost || got[1].Type != domain.TypeParcelLost || !got[0].Params.ToSeller {
		t.Fatalf("%+v", got)
	}
	for want, c := range map[string]struct {
		n   domain.Notice
		tag locale.Tag
	}{
		"GHN làm thất lạc Xe đạp Asama, bạn yêu cầu bồi thường trên tài khoản GHN của mình": {got[0], locale.Vietnamese},
		"GHN làm thất lạc Xe đạp Asama, bạn không phải trả gì":                              {got[1], locale.Vietnamese},
		"GHN lost Xe đạp Asama; claim compensation on your own GHN account":                 {got[0], locale.English},
		"GHN lost Xe đạp Asama, and you pay nothing":                                        {got[1], locale.English},
	} {
		if text := domain.Text(c.n.Type, c.n.Params, c.tag); text != want {
			t.Errorf("%q, want %q", text, want)
		}
	}
	if tg := domain.TargetOf(got[1].Type, got[1].Params); tg != (domain.Target{Kind: "order", ID: order.String()}) {
		t.Errorf("target %+v", tg)
	}
	toSeller, toBuyer := domain.Mail(got[0], locale.Vietnamese), domain.Mail(got[1], locale.Vietnamese)
	if toSeller.Note == "" || len(toSeller.Rows) == 0 || toBuyer.Note != "" || toBuyer.Subject == "" || toSeller.Action == nil {
		t.Fatalf("mail: seller %+v, buyer %+v", toSeller, toBuyer)
	}
}
