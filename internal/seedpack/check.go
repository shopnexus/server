package seedpack

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/platform/contact"
	trustdomain "github.com/shopnexus/server/internal/trust/domain"
)

// CheckAccount applies the server's rules for a person: a name of 1 to 50
// characters, a bio of up to 200, a phone the server parses, and an address
// of a known kind.
func CheckAccount(a Account) error {
	if a.Key == "" {
		return errors.New("an account without a key")
	}
	acc, err := accountdomain.New(uuid.Nil(), a.Name, a.JoinedAt)
	if err == nil {
		err = acc.Edit(&a.Name, &a.Bio, a.JoinedAt)
	}
	if err != nil {
		return fmt.Errorf("account %s: %w", a.Key, err)
	}
	if _, err = contact.ParsePhone(a.Phone); err != nil {
		return fmt.Errorf("account %s: phone %q: %w", a.Key, a.Phone, err)
	}
	if a.JoinedAt.IsZero() {
		return fmt.Errorf("account %s: no joined_at", a.Key)
	}
	if ad := a.Address; ad != nil {
		if !slices.Contains([]string{"home", "work", "other"}, ad.Kind) || ad.Street == "" || ad.Commune == "" {
			return fmt.Errorf("account %s: an address needs a street, a commune and a kind of home, work or other", a.Key)
		}
	}
	return nil
}

// Input is the listing as the server's ListingInput, photos as placeholder
// upload IDs.
func (l Listing) Input() catalogdomain.ListingInput {
	photos := make([]uuid.UUID, len(l.Photos))
	for i := range photos {
		photos[i] = ID("check", "photo", l.Key+"/"+strconv.Itoa(i), time.Time{})
	}
	specs := make([]catalogdomain.Spec, len(l.Specs))
	for i, s := range l.Specs {
		specs[i] = catalogdomain.Spec{Label: s.Label, Value: s.Value}
	}
	return catalogdomain.ListingInput{
		Title: l.Title, Price: l.Price, Description: l.Description, CategoryCode: l.Category,
		Condition: catalogdomain.Condition(l.Condition), AllowOffers: l.AllowOffers, CommuneCode: l.Commune,
		Photos: photos, Specs: specs,
	}
}

// CheckListing applies the server's listing rules through
// catalogdomain.NewListing. Whether the category is a leaf and the commune
// exists needs the database; the loader checks those.
func CheckListing(l Listing) error {
	if l.Key == "" || l.Seller == "" {
		return errors.New("a listing without a key or a seller")
	}
	refs := catalogdomain.Refs{CategoryIsLeaf: strings.Contains(l.Category, "."), CommuneExists: l.Commune != ""}
	if _, err := catalogdomain.NewListing(uuid.Nil(), uuid.Nil(), l.Input(), refs, l.CreatedAt); err != nil {
		return fmt.Errorf("listing %s: %w", l.Key, err)
	}
	if l.CreatedAt.IsZero() {
		return fmt.Errorf("listing %s: no created_at", l.Key)
	}
	for _, sha := range l.Photos {
		if !isSHA(sha) {
			return fmt.Errorf("listing %s: photo %q is not a SHA-256", l.Key, sha)
		}
	}
	return nil
}

// CheckSale applies a listing's rules to the sold listing and checks the
// order: another person buys at a price up to the listed one, ordered after
// the listing and delivered after the order, with reviews of 1 to 5 stars.
func CheckSale(s Sale) error {
	if err := CheckListing(s.Listing); err != nil {
		return err
	}
	k := s.Listing.Key
	switch {
	case s.Buyer == "" || s.Buyer == s.Listing.Seller:
		return fmt.Errorf("sale %s: the buyer must be another person", k)
	case s.Price < catalogdomain.MinPrice || s.Price > s.Listing.Price:
		return fmt.Errorf("sale %s: price %d is not between %d and the listed %d", k, s.Price, catalogdomain.MinPrice, s.Listing.Price)
	case s.OrderedAt.Before(s.Listing.CreatedAt) || !s.DeliveredAt.After(s.OrderedAt):
		return fmt.Errorf("sale %s: want created_at ≤ ordered_at < delivered_at", k)
	case s.ShippedAt != nil && (s.ShippedAt.Before(s.OrderedAt) || s.ShippedAt.After(s.DeliveredAt)):
		return fmt.Errorf("sale %s: shipped_at is not between the order and the delivery", k)
	}
	roles := map[string]bool{}
	for _, r := range s.Reviews {
		if (r.Role != RoleSeller && r.Role != RoleBuyer) || roles[r.Role] {
			return fmt.Errorf("sale %s: one review per role, seller or buyer", k)
		}
		roles[r.Role] = true
		if r.Stars < 1 || r.Stars > 5 || r.At.Before(s.DeliveredAt) {
			return fmt.Errorf("sale %s: a review needs 1 to 5 stars and a time after the delivery", k)
		}
		if r.Reply != nil && (r.Reply.Text == "" || r.Reply.At.Before(r.At)) {
			return fmt.Errorf("sale %s: a reply needs text and a time after its review", k)
		}
		for i, t := range r.Tags {
			if !slices.Contains(trustdomain.Role(r.Role).Tags(), trustdomain.Tag(t)) || slices.Contains(r.Tags[:i], t) {
				return fmt.Errorf("sale %s: tag %q is not one of the %s role's, or comes twice", k, t, r.Role)
			}
		}
	}
	return nil
}

// TextSHA256 is the hex SHA-256 of a listing's EmbeddingText under the
// category's display name.
func TextSHA256(l Listing, categoryName string) string {
	h := sha256.Sum256([]byte(l.Input().EmbeddingText(categoryName)))
	return hex.EncodeToString(h[:])
}

func isSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
