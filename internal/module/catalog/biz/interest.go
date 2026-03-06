package catalogbiz

// ---------- constants ----------

const (
	numInterests   = 4
	mergeThreshold = 0.7
	maxStrength    = 20.0
	minAlpha       = 0.05
)

// ---------- event weights ----------

var eventWeights = map[string]float32{
	// positive signals
	"purchase":                  0.8,
	"add_to_cart":               0.5,
	"view":                      0.3,
	"add_to_favorites":          0.6,
	"write_review":              0.5,
	"rating_high":               0.4,
	"rating_medium":             0.1,
	"ask_question":              0.25,
	"click_from_recommendation": 0.15,
	"click_from_search":         0.2,
	"click_from_category":       0.12,
	"view_similar_products":     0.15,

	// negative signals
	"remove_from_cart": -0.3,
	"return_product":   -0.6,
	"refund_requested": -0.7,
	"cancel_order":     -0.6,
	"rating_low":       -0.5,
	"report_product":   -1.2,
	"dislike":          -0.5,
	"hide_item":        -0.35,
	"not_interested":   -0.3,
	"view_bounce":      -0.1,
}

// getEventWeight returns the weight for a given event type.
// Unknown event types return 0.
func getEventWeight(eventType string) float32 {
	if w, ok := eventWeights[eventType]; ok {
		return w
	}
	return 0
}

// defaultInterests returns a zero-initialised set of interest vectors and
// strengths with the given embedding dimension.
func defaultInterests(dim int) ([][]float32, []float32) {
	interests := make([][]float32, numInterests)
	for i := range interests {
		interests[i] = make([]float32, dim)
	}
	strengths := make([]float32, numInterests)
	return interests, strengths
}

// assignPositive blends productVec into the closest matching interest slot
// using an exponential moving average (EMA). If no slot is close enough the
// vector goes into the first empty slot, or replaces the weakest slot.
//
// interests and strengths are modified in-place.
func assignPositive(interests [][]float32, strengths []float32, productVec []float32, weight float32) {
	bestIdx := -1
	bestSim := float32(-2.0)
	emptyIdx := -1

	for i := range interests {
		if strengths[i] == 0 {
			if emptyIdx == -1 {
				emptyIdx = i
			}
			continue // skip zero-strength slots for similarity check
		}
		sim := cosineSim(interests[i], productVec)
		if sim > bestSim {
			bestSim = sim
			bestIdx = i
		}
	}

	switch {
	case bestIdx != -1 && bestSim > mergeThreshold:
		// EMA blend into the closest slot
		alpha := weight / (strengths[bestIdx] + weight)
		if alpha < minAlpha {
			alpha = minAlpha
		}
		// blended = (1-alpha)*interest + alpha*productVec
		interests[bestIdx] = vectorAdd(
			vectorScale(interests[bestIdx], 1-alpha),
			vectorScale(productVec, alpha),
		)
		interests[bestIdx] = vectorNormalize(interests[bestIdx])
		strengths[bestIdx] += weight
		if strengths[bestIdx] > maxStrength {
			strengths[bestIdx] = maxStrength
		}

	case emptyIdx != -1:
		// Place into the first empty slot
		interests[emptyIdx] = make([]float32, len(productVec))
		copy(interests[emptyIdx], productVec)
		strengths[emptyIdx] = weight

	default:
		// Replace the weakest slot via EMA blend
		weakIdx := 0
		for i := 1; i < len(strengths); i++ {
			if strengths[i] < strengths[weakIdx] {
				weakIdx = i
			}
		}
		alpha := weight / (strengths[weakIdx] + weight)
		if alpha < minAlpha {
			alpha = minAlpha
		}
		interests[weakIdx] = vectorAdd(
			vectorScale(interests[weakIdx], 1-alpha),
			vectorScale(productVec, alpha),
		)
		interests[weakIdx] = vectorNormalize(interests[weakIdx])
		strengths[weakIdx] += weight
		if strengths[weakIdx] > maxStrength {
			strengths[weakIdx] = maxStrength
		}
	}
}

// assignNegative pushes the closest matching interest slot away from
// productVec when the similarity exceeds the merge threshold.
//
// interests and strengths are modified in-place.
func assignNegative(interests [][]float32, strengths []float32, productVec []float32, weight float32) {
	bestIdx := -1
	bestSim := float32(-2.0)

	for i := range interests {
		sim := cosineSim(interests[i], productVec)
		if sim > bestSim {
			bestSim = sim
			bestIdx = i
		}
	}

	if bestIdx == -1 || bestSim <= mergeThreshold {
		return
	}

	// weight is negative; use its absolute value for alpha calculation
	absW := weight
	if absW < 0 {
		absW = -absW
	}
	alpha := absW / (strengths[bestIdx] + absW)
	if alpha < minAlpha {
		alpha = minAlpha
	}

	// Push away: adjusted = interest - alpha * productVec
	adjusted := vectorSub(interests[bestIdx], vectorScale(productVec, alpha))
	interests[bestIdx] = vectorNormalize(adjusted)

	strengths[bestIdx] -= absW
	if strengths[bestIdx] < 0 {
		strengths[bestIdx] = 0
	}
}
