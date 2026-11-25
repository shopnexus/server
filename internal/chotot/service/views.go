package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	catalogdomain "github.com/shopnexus/server/internal/catalog/domain"
	catalogsvc "github.com/shopnexus/server/internal/catalog/service"
	"github.com/shopnexus/server/internal/chotot/domain"
)

// LinkView is the ChototLink schema in api/schemas/chotot.yaml.
type LinkView struct {
	domain.Settings

	OID        string     `json:"oid"`
	Name       string     `json:"name"`
	AvatarURL  *string    `json:"avatar_url"`
	ProfileURL string     `json:"profile_url"`
	LinkedAt   time.Time  `json:"linked_at"`
	CheckedAt  *time.Time `json:"checked_at"`
	Imported   int        `json:"imported"`
	Waiting    *int       `json:"waiting"`
}

// StateView is the ChototState schema.
type StateView struct {
	Link *LinkView `json:"link"`
}

// DraftView is the ChototDraft schema.
type DraftView struct {
	Title        string                      `json:"title"`
	Price        *int64                      `json:"price"`
	Description  string                      `json:"description"`
	CategoryCode *string                     `json:"category_code"`
	Condition    catalogdomain.Condition     `json:"condition"`
	Specs        []catalogdomain.Spec        `json:"specs"`
	Photos       []catalogsvc.DraftPhotoView `json:"photos"`
}

// ImportView is the ChototImport schema.
type ImportView struct {
	ListID          int64              `json:"list_id"`
	State           domain.ImportState `json:"state"`
	ListingID       *uuid.UUID         `json:"listing_id"`
	Review          []string           `json:"review"`
	MissingSpecKeys []string           `json:"missing_spec_keys"`
	Reason          *string            `json:"reason"`
	Draft           *DraftView         `json:"draft"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// ImportListView is the ChototImportList schema.
type ImportListView struct {
	Items []ImportView `json:"items"`
}

// AdView is the ChototAd schema.
type AdView struct {
	ListID    int64          `json:"list_id"`
	Title     string         `json:"title"`
	Price     *int64         `json:"price"`
	ThumbURL  *string        `json:"thumb_url"`
	URL       string         `json:"url"`
	State     domain.AdState `json:"state"`
	Reason    *string        `json:"reason"`
	ListingID *uuid.UUID     `json:"listing_id"`
	Import    *ImportView    `json:"import"`
}

// AdListView is the ChototAdList schema.
type AdListView struct {
	Items []AdView `json:"items"`
	Room  int      `json:"room"`
}

// LinkFacts is a link as notification reads it.
type LinkFacts struct {
	ID      uuid.UUID `json:"id"`
	Account uuid.UUID `json:"account"`
}

// SaleFacts is a sold copy as notification reads it: its import, its
// seller, the copy and its title, and the listing on Chợ Tốt.
type SaleFacts struct {
	ID        uuid.UUID `json:"id"`
	Account   uuid.UUID `json:"account"`
	ListingID uuid.UUID `json:"listing_id"`
	Title     string    `json:"title"`
	ListID    int64     `json:"list_id"`
}

// nonEmpty is s, nil when empty.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// linkView is l with the account's count of imports.
func linkView(l domain.Link, imported int) LinkView {
	return LinkView{
		OID: l.OID, Name: l.Name, AvatarURL: nonEmpty(l.AvatarURL), ProfileURL: domain.ProfileURL(l.OID),
		LinkedAt: l.LinkedAt, CheckedAt: l.CheckedAt, Imported: imported, Waiting: l.Waiting, Settings: l.Settings,
	}
}

// imported counts the account's imports of profile oid that are a listing
// here or a draft, at now.
func imported(imps []domain.Import, oid string, now time.Time) int {
	n := 0
	for k := range imps {
		if i := &imps[k]; i.OID == oid && (i.State == domain.Listed || i.State == domain.Drafted && i.Settled(now)) {
			n++
		}
	}
	return n
}

// importView is i with its draft's photos that are still waiting, each
// with a presigned URL.
func (d Deps) importView(ctx context.Context, i domain.Import, now time.Time) (ImportView, error) {
	v := ImportView{
		ListID: i.ListID, State: i.State, ListingID: i.ListingID, Review: i.Review, MissingSpecKeys: i.MissingSpecKeys,
		Reason: nonEmpty(i.Reason), UpdatedAt: i.UpdatedAt,
	}
	if i.State == domain.Drafted && i.Draft != nil && i.Settled(now) {
		dv, err := d.draftView(ctx, i.AccountID, *i.Draft)
		if err != nil {
			return ImportView{}, err
		}
		v.Draft = &dv
	}
	return v, nil
}

func (d Deps) draftView(ctx context.Context, account uuid.UUID, dr domain.Draft) (DraftView, error) {
	gone, err := d.Media.Gone(ctx, account, dr.Photos)
	if err != nil {
		return DraftView{}, fmt.Errorf("check the draft's photos: %w", err)
	}
	photos := catalogdomain.KeepPhotos(dr.Photos, gone)
	v := DraftView{
		Title: dr.Title, Price: dr.Price, Description: dr.Description, CategoryCode: nonEmpty(dr.CategoryCode),
		Condition: dr.Condition, Specs: dr.Specs, Photos: make([]catalogsvc.DraftPhotoView, 0, len(photos)),
	}
	if v.Specs == nil {
		v.Specs = []catalogdomain.Spec{}
	}
	for _, id := range photos {
		url, err := d.Media.PresignPhoto(ctx, id)
		if err != nil {
			return DraftView{}, fmt.Errorf("presign a photo: %w", err)
		}
		v.Photos = append(v.Photos, catalogsvc.DraftPhotoView{UploadID: id, URL: url})
	}
	return v, nil
}
