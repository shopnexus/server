package ordermodel

import commonmodel "shopnexus-remastered/internal/module/common/model"

var (
	ErrOutOfStock             = commonmodel.NewError("order.out_of_stock", "Sorry, product \"%s\" is out of stock right now")
	ErrOrderItemNotFound      = commonmodel.NewError("order.order_item_not_found", "Sorry, we couldn't find the item you requested")
	ErrPaymentGatewayNotFound = commonmodel.NewError("order.payment_gateway_not_found", "Sorry, we couldn't find the payment gateway you requested")
	ErrRefundAddressRequired  = commonmodel.NewError("order.refund_address_required", "Address is required for pick up method")
	ErrRefundCannotBeUpdated  = commonmodel.NewError("order.refund_cannot_be_updated", "Refund cannot be updated in its current status")
)
