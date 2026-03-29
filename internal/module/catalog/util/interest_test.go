package catalogutil

import (
	"testing"
)

// ---------- AssignPositive: empty slots ----------

func TestAssignPositive_EmptySlot(t *testing.T) {
	interests, strengths := DefaultInterests(3)
	vec := []float32{1, 0, 0}

	AssignPositive(interests, strengths, vec, 0.5)

	// Should land in slot 0 (first empty)
	if strengths[0] != 0.5 {
		t.Fatalf("expected strength 0.5, got %f", strengths[0])
	}
	if interests[0][0] != 1.0 || interests[0][1] != 0.0 || interests[0][2] != 0.0 {
		t.Fatalf("expected [1,0,0], got %v", interests[0])
	}

	// Second different vector should go into slot 1
	vec2 := []float32{0, 1, 0}
	AssignPositive(interests, strengths, vec2, 0.3)
	if strengths[1] != 0.3 {
		t.Fatalf("expected strength 0.3 in slot 1, got %f", strengths[1])
	}
}

// ---------- AssignPositive: merge above threshold ----------

func TestAssignPositive_MergeAboveThreshold(t *testing.T) {
	interests, strengths := DefaultInterests(3)

	// Seed slot 0 with a known vector
	vec := VectorNormalize([]float32{1, 0.1, 0})
	AssignPositive(interests, strengths, vec, 1.0)
	origStrength := strengths[0]

	// Feed a very similar vector — should merge into slot 0
	similar := VectorNormalize([]float32{1, 0.15, 0})
	sim := CosineSim(vec, similar)
	if sim <= MergeThreshold {
		t.Fatalf("test setup error: vectors not similar enough (sim=%f)", sim)
	}

	AssignPositive(interests, strengths, similar, 0.5)

	if strengths[0] <= origStrength {
		t.Fatalf("expected strength to increase from %f, got %f", origStrength, strengths[0])
	}
	// Slot 1 should still be empty
	if strengths[1] != 0 {
		t.Fatalf("expected slot 1 to remain empty, strength=%f", strengths[1])
	}
}

// ---------- AssignNegative: push away ----------

func TestAssignNegative_PushAway(t *testing.T) {
	interests, strengths := DefaultInterests(3)

	// Seed slot 0
	vec := VectorNormalize([]float32{1, 0.1, 0})
	AssignPositive(interests, strengths, vec, 5.0)
	origStrength := strengths[0]

	// Negative signal with a very similar vector
	negVec := VectorNormalize([]float32{1, 0.12, 0})
	sim := CosineSim(interests[0], negVec)
	if sim <= MergeThreshold {
		t.Fatalf("test setup error: vectors not similar enough (sim=%f)", sim)
	}

	AssignNegative(interests, strengths, negVec, -0.6)

	if strengths[0] >= origStrength {
		t.Fatalf("expected strength to decrease from %f, got %f", origStrength, strengths[0])
	}
}

// ---------- AssignPositive: MaxStrength cap ----------

func TestAssignPositive_MaxStrengthCap(t *testing.T) {
	interests, strengths := DefaultInterests(3)

	// Seed slot 0 near the cap
	vec := VectorNormalize([]float32{1, 0, 0})
	interests[0] = make([]float32, 3)
	copy(interests[0], vec)
	strengths[0] = MaxStrength - 0.1

	// Merge a similar vector with weight that would exceed the cap
	AssignPositive(interests, strengths, vec, 5.0)

	if strengths[0] > MaxStrength {
		t.Fatalf("strength %f exceeds MaxStrength %f", strengths[0], MaxStrength)
	}
	if strengths[0] != MaxStrength {
		t.Fatalf("expected strength capped at %f, got %f", MaxStrength, strengths[0])
	}
}
