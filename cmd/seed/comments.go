package main

import (
	"context"
	"fmt"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/google/uuid"

	accountdb "shopnexus-server/internal/module/account/db/sqlc"
	catalogdb "shopnexus-server/internal/module/catalog/db/sqlc"
)

var reviewsPositive = []string{
	"Great product, exactly as described!",
	"Very happy with my purchase. Fast shipping too.",
	"Excellent quality for the price. Would buy again.",
	"Love it! Fits perfectly and looks amazing.",
	"Highly recommend this product to everyone.",
	"Best purchase I've made in a while.",
	"Amazing quality, exceeded my expectations.",
	"Perfect! Just what I was looking for.",
	"Super fast delivery and well packaged.",
	"Five stars! Will definitely order again.",
}

var reviewsNeutral = []string{
	"Product is okay, nothing special.",
	"Decent quality for the price.",
	"It works as expected.",
	"Average product, meets basic needs.",
	"Not bad, but could be better.",
	"Acceptable quality overall.",
	"Does the job. Nothing more, nothing less.",
	"Reasonable for what you pay.",
}

var reviewsNegative = []string{
	"Not as expected, quality could be better.",
	"Disappointed with the product.",
	"Could be improved in many ways.",
	"Not worth the price in my opinion.",
	"Had some issues with the product.",
	"Arrived damaged, but seller was helpful.",
	"Smaller than expected from the photos.",
}

func createComments(
	ctx context.Context,
	fake *gofakeit.Faker,
	catalogStore *catalogdb.Queries,
	accountStore *accountdb.Queries,
	spuID uuid.UUID,
	input InputProduct,
	accounts []SeedAccount,
) error {
	// Use all seed accounts as reviewers
	reviewerIDs := make([]uuid.UUID, 0, len(accounts))
	for _, a := range accounts {
		reviewerIDs = append(reviewerIDs, a.ID)
	}

	// Generate 3-12 reviews per product
	numComments := fake.IntRange(3, 12)
	if input.Reviews > 0 {
		numComments = min(int(input.Reviews), 15)
	}

	for i := 0; i < numComments; i++ {
		accountID := reviewerIDs[fake.IntRange(0, len(reviewerIDs)-1)]

		score := input.Rating
		if score == 0 {
			score = float64(fake.IntRange(1, 5))
		}
		score += (fake.Float64Range(-0.5, 0.5))
		if score < 1 {
			score = 1
		}
		if score > 5 {
			score = 5
		}

		body := generateReviewBody(fake, score)

		_, err := catalogStore.CreateComment(ctx, catalogdb.CreateCommentParams{
			ID:          uuid.New(),
			AccountID:   accountID,
			RefType:     catalogdb.CatalogCommentRefTypeProductSpu,
			RefID:       spuID,
			Body:        body,
			Upvote:      int64(fake.IntRange(0, 80)),
			Downvote:    int64(fake.IntRange(0, 8)),
			Score:       score / 5.0, // normalize to 0-1
			DateCreated: fake.DateRange(time.Now().Add(-90*24*time.Hour), time.Now()),
			DateUpdated: time.Now(),
		})
		if err != nil {
			continue // skip failed comments
		}
	}

	return nil
}

func generateReviewBody(fake *gofakeit.Faker, score float64) string {
	var pool []string
	if score >= 4 {
		pool = reviewsPositive
	} else if score >= 3 {
		pool = reviewsNeutral
	} else {
		pool = reviewsNegative
	}

	body := pool[fake.IntRange(0, len(pool)-1)]

	// Sometimes add a personal touch
	if fake.Bool() {
		extras := []string{
			fmt.Sprintf(" Shipping took %d days.", fake.IntRange(1, 7)),
			fmt.Sprintf(" I've been using it for %d weeks now.", fake.IntRange(1, 8)),
			" The packaging was nice too.",
			" Color matches the picture.",
			" Size is true to description.",
		}
		body += extras[fake.IntRange(0, len(extras)-1)]
	}

	return body
}
