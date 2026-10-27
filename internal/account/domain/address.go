package domain

import (
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/shopnexus/server2/internal/platform/contact"
)

// MaxAddresses is how many addresses one account may keep.
const MaxAddresses = 10

const (
	maxAddressName = 50
	maxStreet      = 200
)

// AddressDraft is an address as the person typed it.
type AddressDraft struct {
	Name        string
	Phone       string
	Street      string
	CommuneCode string
}

// Address is one entry of an account's address book: where a parcel goes.
// Phone is in the +84 form. Which address is the default is the store's to
// keep, since it spans the book; the entity only reports it.
type Address struct {
	id          uuid.UUID
	accountID   uuid.UUID
	name        string
	phone       string
	street      string
	communeCode string
	isDefault   bool
	writeID     uuid.UUID
	createdAt   time.Time
}

// NewAddress checks d and builds an address of owner's. communeExists is
// what the service found in the administrative list. The first write ID is
// the id.
func NewAddress(id, owner uuid.UUID, d AddressDraft, communeExists bool, now time.Time) (Address, error) {
	clean, err := checkAddress(d, communeExists)
	if err != nil {
		return Address{}, err
	}
	return Address{
		id: id, accountID: owner, name: clean.Name, phone: clean.Phone, street: clean.Street,
		communeCode: clean.CommuneCode, writeID: id, createdAt: now,
	}, nil
}

// RestoreAddress rebuilds a stored address. Stores only.
func RestoreAddress(id, owner uuid.UUID, d AddressDraft, isDefault bool, writeID uuid.UUID, createdAt time.Time) Address {
	return Address{
		id: id, accountID: owner, name: d.Name, phone: d.Phone, street: d.Street, communeCode: d.CommuneCode,
		isDefault: isDefault, writeID: writeID, createdAt: createdAt,
	}
}

func (a *Address) ID() uuid.UUID        { return a.id }
func (a *Address) AccountID() uuid.UUID { return a.accountID }
func (a *Address) Name() string         { return a.name }
func (a *Address) Phone() string        { return a.phone }
func (a *Address) Street() string       { return a.street }
func (a *Address) CommuneCode() string  { return a.communeCode }
func (a *Address) IsDefault() bool      { return a.isDefault }
func (a *Address) WriteID() uuid.UUID   { return a.writeID }
func (a *Address) CreatedAt() time.Time { return a.createdAt }

// Saved records that a store wrote the address under writeID. Stores only.
func (a *Address) Saved(writeID uuid.UUID) { a.writeID = writeID }

// StoredAsDefault records that a store made the address its account's
// default. Stores only.
func (a *Address) StoredAsDefault() { a.isDefault = true }

// Edit replaces the address with d under the rules of NewAddress; a refused
// edit leaves it as it was.
func (a *Address) Edit(d AddressDraft, communeExists bool) error {
	clean, err := checkAddress(d, communeExists)
	if err != nil {
		return err
	}
	a.name, a.phone, a.street, a.communeCode = clean.Name, clean.Phone, clean.Street, clean.CommuneCode
	return nil
}

// checkAddress trims d and puts its phone in the +84 form, or returns
// ErrInvalidAddress with an entry per broken field.
func checkAddress(d AddressDraft, communeExists bool) (AddressDraft, error) {
	e := ErrInvalidAddress
	d.Name, d.Street = strings.TrimSpace(d.Name), strings.TrimSpace(d.Street)
	if n := utf8.RuneCountInString(d.Name); n < 1 || n > maxAddressName {
		e = e.WithField("name", "1 to 50 characters")
	}
	p, err := contact.ParsePhone(d.Phone)
	if err != nil {
		e = e.WithField("phone", "a Vietnamese mobile number")
	}
	if n := utf8.RuneCountInString(d.Street); n < 1 || n > maxStreet {
		e = e.WithField("street", "1 to 200 characters")
	}
	if !communeExists {
		e = e.WithField("commune_code", "a commune code from the administrative list")
	}
	if len(e.Fields) > 0 {
		return AddressDraft{}, e
	}
	d.Phone = p.Value()
	return d, nil
}
