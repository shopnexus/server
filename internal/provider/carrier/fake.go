package carrier

import (
	"context"
	"encoding/binary"
	"strconv"
	"strings"
)

// fakeFee is what the fake charges for any parcel, in đồng.
const fakeFee = 22_000

// Fake knows every tracking code but those that start with NOTFOUND, and
// reads each as the same parcel waiting for pickup. It makes GHTK-<number>
// codes and calls nothing off. With CARRIER=fake the dev endpoint moves
// its shipments.
type Fake struct{}

var _ Client = Fake{}

// Create makes the code from the order ID's last four bytes, which are
// random, so a retry gets the same code and two orders almost never share
// one.
func (Fake) Create(_ context.Context, in CreateInput) (Created, error) {
	n := binary.BigEndian.Uint32(in.OrderID[12:16])
	return Created{TrackingCode: "GHTK-" + strconv.FormatUint(uint64(n), 10), Fee: fakeFee}, nil
}

func (Fake) Lookup(_ context.Context, _ Carrier, code string) (Parcel, error) {
	if strings.HasPrefix(code, "NOTFOUND") {
		return Parcel{}, ErrNotFound
	}
	return Parcel{
		RecipientName: "Ng*** V** A", District: "Quận 1, TP. Hồ Chí Minh", COD: 1_500_000,
		Status: StatusPendingPickup, Fee: new(int64(fakeFee)),
	}, nil
}

func (Fake) Cancel(context.Context, Carrier, string) error { return nil }
