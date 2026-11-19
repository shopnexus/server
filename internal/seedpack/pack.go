// Package seedpack reads and writes seed packs: a marketplace's people,
// listings, photos, sold history, follows and vectors as plain files, built
// once from a source and loaded into a dev stack many times. The format is
// in the spec 2026-11-19-seed-packs-design.md; this package is its Go form.
//
// A pack is a directory:
//
//	pack.json                    Manifest
//	accounts.jsonl               one Account per line
//	listings.jsonl               one Listing per line, on sale or hidden
//	history.jsonl                optional, one Sale per line
//	follows.jsonl                optional, one Follow per line
//	photos/<aa>/<sha256>.jpg     a processed photo, long side at most 1600 px
//	thumbs/<aa>/<sha256>.jpg     its thumbnail, long side at most 400 px
//	avatars/<sha256>.jpg         a square of at most 400 px
//	vectors/text.npy             optional, float16 [listings + sales, 1024]
//	vectors/photo.npy            optional, float16 [photos, 1152]
//	vectors/photo.keys.jsonl     optional, one PhotoKey per row of photo.npy
//	README.md                    what is in it, where it came from, license
package seedpack

import "time"

// Format is the version of the layout this package reads and writes.
const Format = 1

// File and directory names inside a pack.
const (
	ManifestFile  = "pack.json"
	AccountsFile  = "accounts.jsonl"
	ListingsFile  = "listings.jsonl"
	HistoryFile   = "history.jsonl"
	FollowsFile   = "follows.jsonl"
	ReadmeFile    = "README.md"
	PhotosDir     = "photos"
	ThumbsDir     = "thumbs"
	AvatarsDir    = "avatars"
	TextVectors   = "vectors/text.npy"
	PhotoVectors  = "vectors/photo.npy"
	PhotoKeysFile = "vectors/photo.keys.jsonl"
)

// Row i of text.npy is line i of listings.jsonl, then line j of
// history.jsonl is row listings+j. A row of zeros has no vector.

// TextDims and PhotoDims are the widths of the vector files: BGE-M3 and
// SigLIP 2 so400m, as the server stores them.
const (
	TextDims  = 1024
	PhotoDims = 1152
)

// OriginLegacy marks text vectors made from an older text of each listing:
// the loader writes them under the hash of the listing's text as it is now.
const OriginLegacy = "legacy"

// Statuses a listing line may carry; empty is active. Sold listings are
// lines of history.jsonl.
const (
	StatusActive = "active"
	StatusHidden = "hidden"
	StatusSold   = "sold"
)

// Roles a review names: the person reviewed.
const (
	RoleSeller = "seller"
	RoleBuyer  = "buyer"
)

// Manifest is pack.json.
type Manifest struct {
	Format  int     `json:"format"`
	Name    string  `json:"name"`
	Version string  `json:"version"`
	License string  `json:"license"`
	Source  string  `json:"source"`
	Counts  Counts  `json:"counts"`
	Vectors Vectors `json:"vectors,omitzero"`
}

// Counts are what the pack holds; verify checks them against the files.
type Counts struct {
	Accounts int `json:"accounts"`
	Listings int `json:"listings"`
	Photos   int `json:"photos"`
	Avatars  int `json:"avatars,omitempty"`
	Sold     int `json:"sold,omitempty"`
	Reviews  int `json:"reviews,omitempty"`
	Follows  int `json:"follows,omitempty"`
}

// Vectors says which vector files the pack has and what made them.
type Vectors struct {
	Text  *VectorSet `json:"text,omitempty"`
	Photo *VectorSet `json:"photo,omitempty"`
}

// VectorSet names the model of one vector file: the server's
// EMBED_TEXT_VERSION or EMBED_IMAGE_VERSION. Origin is OriginLegacy or
// empty.
type VectorSet struct {
	Model  string `json:"model"`
	Origin string `json:"origin,omitempty"`
}

// Account is one person.
type Account struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Bio  string `json:"bio,omitempty"`
	// Phone is in the +84 form, in a dev range (+849001xxxxx), so a dev
	// sign-in that trusts the number reaches this person.
	Phone   string `json:"phone"`
	Commune string `json:"commune"`
	// Avatar is the SHA-256 of a file in avatars/, empty for none.
	Avatar       string     `json:"avatar,omitempty"`
	JoinedAt     time.Time  `json:"joined_at"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
	// Address is the person's one address book entry, nil for none.
	Address *Address `json:"address,omitempty"`
}

// Address is an address book entry; the recipient is the account's name
// and phone.
type Address struct {
	Street  string `json:"street"`
	Commune string `json:"commune"`
	Kind    string `json:"kind"` // home, work or other
}

// Listing is one item, valid for the server's listing rules (title,
// price, leaf category, commune, 1 to 10 photos, specs).
type Listing struct {
	Key         string `json:"key"`
	Seller      string `json:"seller"`
	Title       string `json:"title"`
	Price       int64  `json:"price"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Condition   string `json:"condition"` // new, like_new or used
	AllowOffers bool   `json:"allow_offers"`
	Commune     string `json:"commune"`
	Specs       []Spec `json:"specs"`
	// Photos are SHA-256s of files in photos/ and thumbs/, in order.
	Photos    []string  `json:"photos"`
	CreatedAt time.Time `json:"created_at"`
	// Status is StatusActive (or empty), StatusHidden, or StatusSold in
	// history.jsonl.
	Status string `json:"status,omitempty"`
	// TextSHA256 is the hex SHA-256 of the listing's EmbeddingText when its
	// text vector was made; empty without one or for legacy vectors.
	TextSHA256 string `json:"text_sha256,omitempty"`
}

type Spec struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// Sale is one line of history.jsonl: a listing sold through a deal, its
// delivered cash-on-delivery order and the reviews of both sides.
type Sale struct {
	Listing Listing `json:"listing"`
	Buyer   string  `json:"buyer"`
	// Price is what the two agreed, at most the listed price.
	Price     int64     `json:"price"`
	OrderedAt time.Time `json:"ordered_at"`
	// ShippedAt is optional; the loader puts it between the order and the
	// delivery.
	ShippedAt   *time.Time `json:"shipped_at,omitempty"`
	DeliveredAt time.Time  `json:"delivered_at"`
	Reviews     []Review   `json:"reviews,omitempty"`
}

// Review is one side's review of the other. Role is the role of the person
// reviewed: RoleSeller is the buyer's review of the seller. Text may be
// empty. Reply is the reviewed person's one answer.
type Review struct {
	Role  string    `json:"role"`
	Stars int       `json:"stars"`
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	Reply *Reply    `json:"reply,omitempty"`
}

type Reply struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Follow is one line of follows.jsonl.
type Follow struct {
	Follower string    `json:"follower"`
	Followee string    `json:"followee"`
	At       time.Time `json:"at"`
}

// PhotoKey is one line of vectors/photo.keys.jsonl: row i of photo.npy is
// the vector of photo Photo, first used by listing Listing.
type PhotoKey struct {
	Listing string `json:"listing"`
	Photo   string `json:"photo"`
}

// PhotoPath and ThumbPath are where a photo's files are inside a pack.
func PhotoPath(sha string) string  { return PhotosDir + "/" + sha[:2] + "/" + sha + ".jpg" }
func ThumbPath(sha string) string  { return ThumbsDir + "/" + sha[:2] + "/" + sha + ".jpg" }
func AvatarPath(sha string) string { return AvatarsDir + "/" + sha + ".jpg" }
