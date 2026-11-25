package carrier

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// fakeFee is what the fake charges for any parcel, in đồng.
const fakeFee = 22_000

// Fake is one carrier in development and tests. It links any account but
// the password, token or code "wrong", texts a code to any phone but one
// ending in 0000, knows every code but those that start
// with NOTFOUND, reads each as the same parcel waiting for pickup, and
// makes codes from the order code, so a retry gets the same one. It reads
// reports as its carrier posts them. With CARRIER_GHTK or CARRIER_GHN
// fake the dev endpoint moves its parcels.
type Fake struct{ Carrier Carrier }

var (
	_ Client = Fake{}
	_ Texter = Fake{}
)

func (Fake) SendOTP(_ context.Context, phone string) error {
	if strings.HasSuffix(phone, "0000") {
		return ErrPhoneUnknown
	}
	return nil
}

func (f Fake) Link(_ context.Context, c Credentials) (Linked, error) {
	if c.Password == "wrong" || c.Token == "wrong" {
		return Linked{}, ErrLoginFailed
	}
	if c.OTP == "wrong" {
		return Linked{}, ErrOTPInvalid
	}
	if c.OTP != "" {
		sum := sha256.Sum256([]byte(c.Phone))
		return Linked{ShopCode: strconv.Itoa(int(binary.BigEndian.Uint16(sum[:2])))}, nil
	}
	if f.Carrier == GHN {
		return Linked{ShopCode: c.ShopID, Token: "fake-ghn-" + c.Token}, nil
	}
	sum := sha256.Sum256([]byte(c.Email))
	return Linked{ShopCode: "S" + strconv.Itoa(int(binary.BigEndian.Uint16(sum[:2]))), Token: "fake-ghtk-" + c.Email}, nil
}

func (Fake) Pickups(context.Context, Account) ([]PickupPoint, error) {
	return []PickupPoint{
		{ID: "88256", Name: "Kho Bình Thạnh", Phone: "0987654321", Address: "12 Nguyễn Gia Trí, Phường 25, Bình Thạnh"},
		{ID: "88260", Name: "Kho Quận 1", Phone: "0987654321", Address: "1 Lê Duẩn, Bến Nghé, Quận 1"},
	}, nil
}

func (Fake) Quote(context.Context, Account, QuoteInput) (int64, error) { return fakeFee, nil }

// Create makes the code from a hash of the order code.
func (f Fake) Create(_ context.Context, _ Account, in CreateInput) (Created, error) {
	sum := sha256.Sum256([]byte(in.OrderCode))
	n := binary.BigEndian.Uint32(sum[:4])
	return Created{TrackingCode: strings.ToUpper(string(f.Carrier)) + "-" + strconv.FormatUint(uint64(n), 10), Fee: fakeFee}, nil
}

func (Fake) Lookup(_ context.Context, _ Account, code string) (Parcel, error) {
	if strings.HasPrefix(code, "NOTFOUND") {
		return Parcel{}, ErrNotFound
	}
	return Parcel{
		RecipientName: "Ng*** V** A", District: "Quận 1, TP. Hồ Chí Minh", COD: 1_500_000, Fee: new(int64(fakeFee)),
	}, nil
}

func (Fake) Cancel(context.Context, Account, string) error { return nil }

// Report reads a report as the carrier the fake stands in for posts it.
func (f Fake) Report(contentType string, body []byte, now time.Time) (string, []Event, error) {
	switch f.Carrier {
	case GHTK:
		return GHTKClient{}.Report(contentType, body, now)
	case GHN:
		return GHNClient{}.Report(contentType, body, now)
	}
	return "", nil, fmt.Errorf("no reports from %s", f.Carrier)
}
