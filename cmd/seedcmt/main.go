package main

import (
	"context"
	"log"
	"math/rand"
	"time"

	"shopnexus-remastered/config"
	"shopnexus-remastered/internal/infras/pg"
	accountdb "shopnexus-remastered/internal/module/account/db/sqlc"
	catalogdb "shopnexus-remastered/internal/module/catalog/db/sqlc"

	"github.com/google/uuid"
	null "github.com/guregu/null/v6"
)

// generateParagraph generates a random paragraph using simple lorem ipsum style text
func generateParagraph() string {
	words := []string{
		"lorem", "ipsum", "dolor", "sit", "amet", "consectetur", "adipiscing", "elit",
		"sed", "do", "eiusmod", "tempor", "incididunt", "ut", "labore", "et", "dolore",
		"magna", "aliqua", "enim", "ad", "minim", "veniam", "quis", "nostrud",
		"exercitation", "ullamco", "laboris", "nisi", "aliquip", "ex", "ea", "commodo",
		"consequat", "duis", "aute", "irure", "in", "reprehenderit", "voluptate",
		"velit", "esse", "cillum", "fugiat", "nulla", "pariatur", "excepteur", "sint",
		"occaecat", "cupidatat", "non", "proident", "sunt", "culpa", "qui", "officia",
		"deserunt", "mollit", "anim", "id", "est", "laborum",
	}

	// Generate 3-8 sentences
	numSentences := rand.Intn(6) + 3
	var paragraph string

	for range numSentences {
		// Each sentence has 8-15 words
		sentenceLen := rand.Intn(8) + 8
		var sentence string
		for w := range sentenceLen {
			word := words[rand.Intn(len(words))]
			if w == 0 {
				// Capitalize first letter
				word = string(word[0]-32) + word[1:]
			}
			if w > 0 {
				sentence += " "
			}
			sentence += word
		}
		sentence += ". "
		paragraph += sentence
	}

	return paragraph
}

func main() {
	ctx := context.Background()
	cfg := config.GetConfig()

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

	accountStore := accountdb.New(pool)
	catalogStore := catalogdb.New(pool)

	// Get the first available account to use as comment author
	accounts, err := accountStore.ListAccount(ctx, accountdb.ListAccountParams{
		Limit: null.Int32From(1),
	})
	if err != nil || len(accounts) == 0 {
		log.Fatalf("failed to get an account: %v", err)
	}
	accountID := accounts[0].ID

	log.Printf("Using account ID: %s for comments", accountID)

	// Get all ProductSpus
	spus, err := catalogStore.ListProductSpu(ctx, catalogdb.ListProductSpuParams{})
	if err != nil {
		log.Fatalf("failed to list product spus: %v", err)
	}

	log.Printf("Found %d product SPUs", len(spus))

	// Create comments for each SPU
	var commentsCreated int
	for i, spu := range spus {
		// Create 5-10 comments per SPU (matching the TS faker.number.int({ min: 5, max: 10 }))
		numComments := rand.Intn(6) + 5

		for range numComments {
			_, err := catalogStore.CreateComment(ctx, catalogdb.CreateCommentParams{
				ID:          uuid.New(),
				AccountID:   accountID,
				RefType:     catalogdb.CatalogCommentRefTypeProductSpu,
				RefID:       spu.ID,
				Body:        generateParagraph(),
				Upvote:      int64(rand.Intn(100) + 1),   // 1-100
				Downvote:    int64(rand.Intn(10)),        // 0-10
				Score:       float64(rand.Intn(100) + 1), // 1-100
				DateCreated: time.Now(),
				DateUpdated: time.Now(),
			})
			if err != nil {
				log.Printf("Warning: failed to create comment for SPU %s: %v", spu.ID, err)
				continue
			}
			commentsCreated++
		}

		if (i+1)%100 == 0 {
			log.Printf("Progress: %d/%d SPUs processed", i+1, len(spus))
		}
	}

	log.Printf("✅ Successfully created %d comments for %d product SPUs", commentsCreated, len(spus))
}
