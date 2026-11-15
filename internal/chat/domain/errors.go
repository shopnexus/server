package domain

import (
	"errors"

	"github.com/shopnexus/server2/internal/platform/apperr"
)

// ErrMisplacedMove is a move posted to a conversation that is not its
// pair's, by someone who is neither a member nor ShopNexus. Posting it
// again cannot succeed.
var ErrMisplacedMove = errors.New("chat: the move does not belong in the conversation")

var (
	ErrNotFound            = apperr.New(apperr.KindNotFound, "conversation_not_found", "no such conversation of the caller's")
	ErrInvalidMessage      = apperr.New(apperr.KindInvalid, "invalid_message", "the message breaks a rule; see fields")
	ErrOwnListing          = apperr.New(apperr.KindConflict, "own_listing", "a seller cannot open a conversation on their own listing")
	ErrListingUnavailable  = apperr.New(apperr.KindConflict, "listing_unavailable", "the listing is not on sale")
	ErrSupportConversation = apperr.New(apperr.KindConflict, "conversation_is_support",
		"a support chat cannot be archived, marked, blocked or reported")
	ErrInvalidQuery = apperr.New(apperr.KindInvalid, "invalid_query", "a search needs 1 to 100 characters with a letter or a digit")
)
