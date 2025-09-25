package ordermodel

import sharedmodel "shopnexus-remastered/internal/module/shared/model"

var (
	ErrOutOfStock             = sharedmodel.NewError("order.out_of_stock", "Sorry, product \"%s\" is out of stock right now")
	ErrOrderItemNotFound      = sharedmodel.NewError("order.order_item_not_found", "Sorry, we couldn't find the item you requested")
	ErrPaymentGatewayNotFound = sharedmodel.NewError("order.payment_gateway_not_found", "Sorry, we couldn't find the payment gateway you requested")
)
