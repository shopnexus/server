package service

import (
	"context"
	"fmt"
	"slices"
	"uuid"

	accountdomain "github.com/shopnexus/server/internal/account/domain"
	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	"github.com/shopnexus/server/internal/chat/domain"
)

// RefView is a ShopNexus link in a message as the client draws it, a card in
// place of the URL: the MessageRef schema. URL is the link as written in the
// text. One of the kind's fields is set.
type RefView struct {
	URL      string          `json:"url"`
	Kind     domain.LinkKind `json:"kind"`
	Listing  *RefListing     `json:"listing,omitempty"`
	Seller   *RefSeller      `json:"seller,omitempty"`
	Order    *RefOrder       `json:"order,omitempty"`
	Category *RefCategory    `json:"category,omitempty"`
	Search   *RefSearch      `json:"search,omitempty"`
}

// RefListing is a linked listing as it reads now.
type RefListing struct {
	ID       uuid.UUID            `json:"id"`
	Title    string               `json:"title"`
	Price    int64                `json:"price"`
	Status   catalogdomain.Status `json:"status"`
	ThumbURL string               `json:"thumb_url,omitempty"`
}

// RefSeller is a linked person's public name and photo.
type RefSeller struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url,omitempty"`
}

// RefOrder is a linked order, for its buyer, its seller and ShopNexus only.
type RefOrder struct {
	ID       uuid.UUID `json:"id"`
	Code     string    `json:"code"`
	Status   string    `json:"status"`
	Price    int64     `json:"price"`
	Title    string    `json:"title"`
	ThumbURL string    `json:"thumb_url,omitempty"`
}

// RefCategory is a linked category's name.
type RefCategory struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// RefSearch is a linked search.
type RefSearch struct {
	Query string `json:"query"`
}

// OrderRef is an order as a card needs it, from module deal, which chat
// cannot import: its two sides, its listing, code, status and price.
type OrderRef struct {
	BuyerID, SellerID, ListingID uuid.UUID
	Code, Status                 string
	Price                        int64
}

// OrderRefs reads orders for their cards; an unknown one is left out.
type OrderRefs func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]OrderRef, error)

// refs adds to each view the cards of the ShopNexus links in its text, as
// viewer may see them. Each kind is read once for the page. A link to
// something gone, hidden or not viewer's stays plain text.
func (c *Chat) refs(ctx context.Context, viewer uuid.UUID, views []MessageView) error {
	links := make([][]domain.Link, len(views))
	var listings, people, orders []uuid.UUID
	found, categories := false, false
	for i := range views {
		links[i] = domain.Links(views[i].Text, c.linkHosts)
		found = found || len(links[i]) > 0
		for _, l := range links[i] {
			switch l.Kind {
			case domain.LinkListing:
				listings = append(listings, l.ID)
			case domain.LinkSeller:
				people = append(people, l.ID)
			case domain.LinkOrder:
				orders = append(orders, l.ID)
			case domain.LinkCategory:
				categories = true
			case domain.LinkSearch:
			default:
			}
		}
	}
	if !found {
		return nil
	}
	var ord map[uuid.UUID]OrderRef
	if len(orders) > 0 && c.orders != nil {
		var err error
		if ord, err = c.orders(ctx, orders); err != nil {
			return fmt.Errorf("read the linked orders: %w", err)
		}
		for id, o := range ord {
			if viewer != o.BuyerID && viewer != o.SellerID && viewer != accountdomain.ShopNexusID {
				delete(ord, id)
				continue
			}
			listings = append(listings, o.ListingID)
		}
	}
	sums, err := c.catalog.Summaries(ctx, listings)
	if err != nil {
		return fmt.Errorf("read the linked listings: %w", err)
	}
	accounts, err := c.accounts.GetMany(ctx, people)
	if err != nil {
		return fmt.Errorf("read the linked people: %w", err)
	}
	names := map[string]string{}
	if categories {
		all, err := c.catalog.Categories(ctx)
		if err != nil {
			return fmt.Errorf("read the categories: %w", err)
		}
		for _, cat := range all {
			names[cat.Code] = cat.DisplayName
		}
	}
	for i := range views {
		for _, l := range links[i] {
			r := RefView{URL: l.URL, Kind: l.Kind}
			switch l.Kind {
			case domain.LinkListing:
				s, ok := sums[l.ID]
				if !ok || !slices.Contains(catalogdomain.Public, s.Status) {
					continue
				}
				r.Listing = &RefListing{ID: s.ID, Title: s.Title, Price: s.Price, Status: s.Status, ThumbURL: s.CoverThumbURL}
			case domain.LinkSeller:
				a, ok := accounts[l.ID]
				if !ok || a.Status == accountdomain.StatusDeleted {
					continue
				}
				r.Seller = &RefSeller{ID: l.ID, Name: a.DisplayName, AvatarURL: a.AvatarURL}
			case domain.LinkOrder:
				o, ok := ord[l.ID]
				if !ok {
					continue
				}
				s := sums[o.ListingID]
				r.Order = &RefOrder{ID: l.ID, Code: o.Code, Status: o.Status, Price: o.Price, Title: s.Title, ThumbURL: s.CoverThumbURL}
			case domain.LinkCategory:
				name, ok := names[l.Code]
				if !ok {
					continue
				}
				r.Category = &RefCategory{Code: l.Code, Name: name}
			case domain.LinkSearch:
				r.Search = &RefSearch{Query: l.Query}
			default:
				continue
			}
			views[i].Refs = append(views[i].Refs, r)
		}
	}
	return nil
}
