package ordermodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the order module.
var (
	ErrOrderItemNotFound      = sharedmodel.NewError(http.StatusNotFound, "Sorry, we couldn't find the item you requested")
	ErrPaymentGatewayNotFound = sharedmodel.NewError(http.StatusNotFound, "Sorry, we couldn't find the payment gateway you requested")
	ErrRefundAddressRequired  = sharedmodel.NewError(http.StatusBadRequest, "Address is required for pick up method")
	ErrRefundCannotBeUpdated  = sharedmodel.NewError(http.StatusConflict, "Refund cannot be updated in its current status")
	ErrBuyNowSingleSkuOnly    = sharedmodel.NewError(http.StatusBadRequest, "Buy now is only available for a single product")
	ErrOrderNotFound          = sharedmodel.NewError(http.StatusNotFound, "The order could not be found")
	ErrQuantityParamRequired  = sharedmodel.NewError(http.StatusBadRequest, "Either quantity or delta_quantity must be provided")
	ErrBuyNowQuantityRequired = sharedmodel.NewError(http.StatusBadRequest, "Quantity is required for buy now checkout")
	ErrSkuNotFoundInCart      = sharedmodel.NewError(http.StatusNotFound, "Some SKU not found in cart")
	ErrPaymentCannotCancel    = sharedmodel.NewError(http.StatusConflict, "Payment cannot be canceled")
	ErrOrderCannotCancel      = sharedmodel.NewError(http.StatusConflict, "Order cannot be canceled")
	ErrOrderNotConfirmable    = sharedmodel.NewError(http.StatusConflict, "Order is not in a confirmable state")
	ErrMissingPayment         = sharedmodel.NewError(http.StatusNotFound, "Payment record not found for order")
	ErrMissingPromotedPrice   = sharedmodel.NewError(http.StatusNotFound, "Promoted price not found for SKU")
	ErrUnknownPaymentOption   = sharedmodel.NewError(http.StatusBadRequest, "Unknown payment option: %s")

	// New errors for checkout/order refactor
	ErrItemsNotSameBuyer      = sharedmodel.NewError(http.StatusBadRequest, "all items must belong to the same buyer").Terminal()
	ErrItemsNotSameAddress    = sharedmodel.NewError(http.StatusBadRequest, "all items must have the same address").Terminal()
	ErrItemNotPending         = sharedmodel.NewError(http.StatusBadRequest, "item is not in pending status").Terminal()
	ErrItemNotOwnedBySeller   = sharedmodel.NewError(http.StatusForbidden, "item does not belong to this seller").Terminal()
	ErrOrderNotPayable        = sharedmodel.NewError(http.StatusBadRequest, "order is not payable").Terminal()
	ErrOrderAlreadyPaid       = sharedmodel.NewError(http.StatusBadRequest, "order is already paid").Terminal()
	ErrUnknownTransportOption = sharedmodel.NewError(http.StatusBadRequest, "unknown transport option").Terminal()
)
