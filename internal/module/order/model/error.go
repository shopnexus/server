package ordermodel

import (
	"net/http"

	sharedmodel "shopnexus-server/internal/shared/model"
)

// Sentinel errors for the order module.
var (
	ErrOrderItemNotFound = sharedmodel.NewError(
		http.StatusNotFound,
		"order_item_not_found",
		"Sorry, we couldn't find the item you requested",
	)
	ErrPaymentGatewayNotFound = sharedmodel.NewError(
		http.StatusNotFound,
		"payment_gateway_not_found",
		"Sorry, we couldn't find the payment gateway you requested",
	)
	ErrRefundAddressRequired = sharedmodel.NewError(http.StatusBadRequest, "refund_address_required", "Address is required for pick up method")
	ErrRefundCannotBeUpdated = sharedmodel.NewError(
		http.StatusConflict,
		"refund_cannot_be_updated",
		"Refund cannot be updated in its current status",
	)
	ErrRefundDuplicateItem = sharedmodel.NewError(http.StatusBadRequest, "refund_duplicate_item", "Duplicate item IDs in refund request")
	ErrBuyNowSingleSkuOnly = sharedmodel.NewError(
		http.StatusBadRequest,
		"buy_now_single_sku_only",
		"Buy now is only available for a single product",
	)
	ErrOrderNotFound         = sharedmodel.NewError(http.StatusNotFound, "order_not_found", "The order could not be found")
	ErrQuantityParamRequired = sharedmodel.NewError(
		http.StatusBadRequest,
		"quantity_param_required",
		"Either quantity or delta_quantity must be provided",
	)
	ErrBuyNowQuantityRequired = sharedmodel.NewError(http.StatusBadRequest, "buy_now_quantity_required", "Quantity is required for buy now checkout")
	ErrSkuNotFoundInCart      = sharedmodel.NewError(http.StatusNotFound, "sku_not_found_in_cart", "Some SKU not found in cart")
	ErrPaymentCannotCancel    = sharedmodel.NewError(http.StatusConflict, "payment_cannot_cancel", "Payment cannot be canceled")
	ErrOrderCannotCancel      = sharedmodel.NewError(http.StatusConflict, "order_cannot_cancel", "Order cannot be canceled")
	ErrOrderNotConfirmable    = sharedmodel.NewError(http.StatusConflict, "order_not_confirmable", "Order is not in a confirmable state")
	ErrMissingPayment         = sharedmodel.NewError(http.StatusNotFound, "missing_payment", "Payment record not found for order")
	ErrMissingPromotedPrice   = sharedmodel.NewError(http.StatusNotFound, "missing_promoted_price", "Promoted price not found for SKU")

	ErrItemsNotSameBuyer      = sharedmodel.NewError(http.StatusBadRequest, "items_not_same_buyer", "all items must belong to the same buyer")
	ErrItemsNotSameAddress    = sharedmodel.NewError(http.StatusBadRequest, "items_not_same_address", "all items must have the same address")
	ErrItemNotPending         = sharedmodel.NewError(http.StatusBadRequest, "item_not_pending", "item is not in pending status")
	ErrItemNotOwnedBySeller   = sharedmodel.NewError(http.StatusForbidden, "item_not_owned_by_seller", "item does not belong to this seller")
	ErrOrderNotPayable        = sharedmodel.NewError(http.StatusBadRequest, "order_not_payable", "order is not payable")
	ErrOrderAlreadyPaid       = sharedmodel.NewError(http.StatusBadRequest, "order_already_paid", "order is already paid")
	ErrUnknownTransportOption = sharedmodel.NewError(http.StatusBadRequest, "unknown_transport_option", "unknown transport option")
	ErrNoDefaultPaymentMethod = sharedmodel.NewError(http.StatusBadRequest, "no_default_payment_method", "no default payment method configured")
	ErrPaymentMethodNotFound  = sharedmodel.NewError(http.StatusNotFound, "payment_method_not_found", "payment method not found")

	ErrDisputeNotFound       = sharedmodel.NewError(http.StatusNotFound, "dispute_not_found", "dispute not found")
	ErrDisputeRefundResolved = sharedmodel.NewError(
		http.StatusConflict,
		"dispute_refund_resolved",
		"cannot dispute a refund that has already been resolved or cancelled",
	)
	ErrDisputeAlreadyActive = sharedmodel.NewError(
		http.StatusConflict,
		"dispute_already_active",
		"an active dispute already exists for this refund",
	)
	ErrDisputeNotAuthorized = sharedmodel.NewError(
		http.StatusForbidden,
		"dispute_not_authorized",
		"you are not authorized to access this dispute",
	)

	ErrRefundAmountExceedsPaid = sharedmodel.NewError(
		http.StatusBadRequest,
		"refund_amount_exceeds_paid",
		"refund amount exceeds the total paid amount of the specified items",
	)
	ErrItemNotInOrder = sharedmodel.NewError(
		http.StatusBadRequest,
		"item_not_in_order",
		"one or more item IDs do not belong to the specified order",
	)
	ErrPaymentNotSuccess = sharedmodel.NewError(
		http.StatusBadRequest,
		"payment_not_success",
		"payment has not been completed successfully",
	)
	ErrPaymentExpired = sharedmodel.NewError(
		http.StatusConflict,
		"payment_expired",
		"payment session has expired",
	)
	ErrItemAlreadyCancelled   = sharedmodel.NewError(http.StatusConflict, "item_already_cancelled", "item already cancelled")
	ErrItemAlreadyConfirmed   = sharedmodel.NewError(http.StatusConflict, "item_already_confirmed", "item already confirmed in an order")
	ErrItemsTransportMismatch = sharedmodel.NewError(http.StatusBadRequest, "items_transport_mismatch", "all items must have the same transport option")
	ErrPaymentTimeout         = sharedmodel.NewError(http.StatusConflict, "payment_timeout", "payment session expired")
	ErrSellerConfirmTimeout   = sharedmodel.NewError(http.StatusConflict, "seller_confirm_timeout", "seller confirmation expired")

	ErrUnknownPaymentOption = sharedmodel.NewError(http.StatusBadRequest, "unknown_payment_option", "Unknown payment option: %s")

	ErrCheckoutAddressCountryMismatch = sharedmodel.NewError(
		http.StatusBadRequest,
		"address_country_mismatch",
		"address resolves to %s, buyer country is %s",
	)
	ErrMixedCurrencyCart = sharedmodel.NewError(
		http.StatusBadRequest,
		"mixed_currency_cart",
		"all items must share the same currency (got %s and %s)",
	)
	ErrFXRateUnavailable = sharedmodel.NewError(
		http.StatusServiceUnavailable,
		"fx_rate_unavailable",
		"fx rate unavailable for %s",
	)
	ErrTransportStatusInvalid = sharedmodel.NewError(
		http.StatusConflict,
		"transport_status_invalid",
		"cannot transition transport from %s to %s",
	)
)
