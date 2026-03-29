package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/brianvoe/gofakeit/v7"

	"shopnexus-server/config"
	"shopnexus-server/internal/infras/pg"
	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
	commondb "shopnexus-server/internal/module/common/db/sqlc"
	inventorydb "shopnexus-server/internal/module/inventory/db/sqlc"
	promotiondb "shopnexus-server/internal/module/promotion/db/sqlc"
)

func main() {
	ctx := context.Background()
	cfg := config.GetConfig()
	fake := gofakeit.New(0)

	pool, err := pg.New(pg.Options{
		Url:             cfg.Postgres.Url,
		Host:            cfg.Postgres.Host,
		Port:            cfg.Postgres.Port,
		Username:        cfg.Postgres.Username,
		Password:        cfg.Postgres.Password,
		Database:        cfg.Postgres.Database,
		MaxConnections:  cfg.Postgres.MaxConnections,
		MaxConnIdleTime: cfg.Postgres.MaxConnIdleTime,
	})
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	// Load product data
	data, err := os.ReadFile("data.json")
	if err != nil {
		log.Fatalf("failed to read data.json: %v", err)
	}

	var products []InputProduct
	if err := json.Unmarshal(data, &products); err != nil {
		log.Fatalf("failed to parse data.json: %v", err)
	}

	log.Printf("Loaded %d products from data.json", len(products))

	// Step 1: Create vendor accounts
	accountStore := accountdb.New(pool)
	accounts, err := createAccounts(ctx, accountStore, fake)
	if err != nil {
		log.Fatalf("failed to create accounts: %v", err)
	}

	log.Printf("Created %d vendor accounts:", len(accounts))
	for _, a := range accounts {
		log.Printf("  %s | %s | password: %s", a.Username, a.Email, a.Password)
	}

	// Step 2: Process products — distribute across accounts
	processedCount := 0
	totalProducts := len(products)

	for i, product := range products {
		// Round-robin assign products to accounts
		owner := accounts[i%len(accounts)]

		err := func() error {
			tx, err := pool.Begin(ctx)
			if err != nil {
				return fmt.Errorf("begin tx: %w", err)
			}
			defer tx.Rollback(ctx)

			if err := processProduct(ctx, fake, product, owner.ID, accounts,
				accountdb.New(tx), catalogdb.New(tx), commondb.New(tx), inventorydb.New(tx), promotiondb.New(tx),
			); err != nil {
				return err
			}
			return tx.Commit(ctx)
		}()
		if err != nil {
			log.Printf("Error processing product %d (%s): %v", i+1, product.Title, err)
		} else {
			processedCount++
		}

		if (i+1)%100 == 0 {
			log.Printf("Progress: %d/%d products processed", i+1, totalProducts)
		}
	}

	log.Printf("Successfully processed %d/%d products", processedCount, totalProducts)
}
