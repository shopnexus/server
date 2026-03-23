package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"shopnexus-server/config"
	accountbiz "shopnexus-server/internal/module/account/biz"
	analyticbiz "shopnexus-server/internal/module/analytic/biz"
	catalogbiz "shopnexus-server/internal/module/catalog/biz"
	chatbiz "shopnexus-server/internal/module/chat/biz"
	inventorybiz "shopnexus-server/internal/module/inventory/biz"
	orderbiz "shopnexus-server/internal/module/order/biz"
	promotionbiz "shopnexus-server/internal/module/promotion/biz"

	restate "github.com/restatedev/sdk-go"
	"github.com/restatedev/sdk-go/server"
)

func SetupRestate(
	cfg *config.Config,
	orderBiz *orderbiz.OrderBiz,
	accountBiz *accountbiz.AccountBiz,
	catalogBiz *catalogbiz.CatalogBiz,
	inventoryBiz *inventorybiz.InventoryBiz,
	promotionBiz *promotionbiz.PromotionBiz,
	analyticBiz *analyticbiz.AnalyticBiz,
	chatBiz *chatbiz.ChatBiz,
) {
	bindAddress := fmt.Sprintf("localhost:%s", cfg.Restate.ServicePort)

	srv := server.NewRestate().
		Bind(restate.Reflect(orderBiz)).
		Bind(restate.Reflect(accountBiz)).
		Bind(restate.Reflect(catalogBiz)).
		Bind(restate.Reflect(inventoryBiz)).
		Bind(restate.Reflect(promotionBiz)).
		Bind(restate.Reflect(analyticBiz)).
		Bind(restate.Reflect(chatBiz))

	go func() {
		slog.Info("Starting Restate server on port", "port", bindAddress)
		if err := srv.Start(context.Background(), bindAddress); err != nil {
			slog.Error("Restate server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()
}
