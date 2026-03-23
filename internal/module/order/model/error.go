package ordermodel

import sharedmodel "shopnexus-server/internal/shared/model"

// Sentinel errors for the order module.
var (
	ErrOrderItemNotFound      = sharedmodel.NewError("order.order_item_not_found", "Sorry, we couldn't find the item you requested")
	ErrPaymentGatewayNotFound = sharedmodel.NewError("order.payment_gateway_not_found", "Sorry, we couldn't find the payment gateway you requested")
	ErrRefundAddressRequired  = sharedmodel.NewError("order.refund_address_required", "Address is required for pick up method")
	ErrRefundCannotBeUpdated  = sharedmodel.NewError("order.refund_cannot_be_updated", "Refund cannot be updated in its current status")
	ErrBuyNowSingleSkuOnly    = sharedmodel.NewError("order.buy_now_single_sku", "Buy now is only available for a single product")
	ErrOrderNotFound          = sharedmodel.NewError("order.order_not_found", "The order could not be found")
	ErrQuantityParamRequired  = sharedmodel.NewError("order.quantity_required", "Either quantity or delta_quantity must be provided")
	ErrBuyNowQuantityRequired = sharedmodel.NewError("order.buy_now_quantity_required", "Quantity is required for buy now checkout")
	ErrSkuNotFoundInCart      = sharedmodel.NewError("order.sku_not_found_in_cart", "Some SKU not found in cart")
	ErrPaymentCannotCancel    = sharedmodel.NewError("order.payment_cannot_cancel", "Payment cannot be canceled")
	ErrShipmentCannotCancel   = sharedmodel.NewError("order.shipment_cannot_cancel", "Shipment cannot be canceled")
	ErrOrderCannotCancel      = sharedmodel.NewError("order.order_cannot_cancel", "Order cannot be canceled")
	ErrOrderNotConfirmable    = sharedmodel.NewError("order.order_not_confirmable", "Order is not in a confirmable state")
	ErrMissingPayment         = sharedmodel.NewError("order.missing_payment", "Payment record not found for order")
	ErrMissingShippingQuote   = sharedmodel.NewError("order.missing_shipping_quote", "Shipping quote not found for SKU")
	ErrMissingPromotedPrice   = sharedmodel.NewError("order.missing_promoted_price", "Promoted price not found for SKU")
	ErrUnknownPaymentOption   = sharedmodel.NewError("order.unknown_payment_option", "Unknown payment option: %s")
	ErrUnknownShipmentOption  = sharedmodel.NewError("order.unknown_shipment_option", "Unknown shipment option: %s")
)
