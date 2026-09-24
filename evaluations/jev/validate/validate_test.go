package validate

import (
	"math"
	"testing"
)

func TestDecisionRejectsEmptyChoice(t *testing.T) {
	err := Decision(DecisionObservation{Choice: "", ResolvedModel: "x", Attempts: 1}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecisionAcceptsAbsentOptionalFields(t *testing.T) {
	err := Decision(DecisionObservation{Choice: "a", ResolvedModel: "x", Attempts: 1}, []string{"a"})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestDecisionRejectsVocabularyMismatch(t *testing.T) {
	err := Decision(DecisionObservation{Choice: "martian", ResolvedModel: "x", Attempts: 1}, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecisionRejectsConfidenceOutOfRange(t *testing.T) {
	v := -0.1
	err := Decision(DecisionObservation{Choice: "a", ResolvedModel: "x", Attempts: 1, Confidence: &v}, []string{"a"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecisionRejectsProbabilityNaN(t *testing.T) {
	err := Decision(DecisionObservation{
		Choice: "a", ResolvedModel: "x", Attempts: 1,
		Probabilities: map[string]float64{"a": math.NaN()},
	}, []string{"a"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecisionRejectsProbabilityOutOfRange(t *testing.T) {
	err := Decision(DecisionObservation{
		Choice: "a", ResolvedModel: "x", Attempts: 1,
		Probabilities: map[string]float64{"a": 1.5},
	}, []string{"a"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDecisionRejectsProbabilitySumOff(t *testing.T) {
	err := Decision(DecisionObservation{
		Choice: "a", ResolvedModel: "x", Attempts: 1,
		Probabilities: map[string]float64{"a": 0.2, "b": 0.1},
	}, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProbabilitiesAcceptsValid(t *testing.T) {
	err := Probabilities(map[string]float64{"a": 0.5, "b": 0.5})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestChatRejectsEmptyContent(t *testing.T) {
	err := Chat(ChatObservation{ResolvedModel: "x", Attempts: 1, Content: map[string]any{}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChatRejectsNonFiniteNumeric(t *testing.T) {
	err := Chat(ChatObservation{
		ResolvedModel: "x",
		Attempts:      1,
		Content:       map[string]any{"x": math.Inf(1)},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFixtureIDRejectsWhitespace(t *testing.T) {
	if err := FixtureID("a b"); err == nil {
		t.Fatal("expected error")
	}
	if err := FixtureID(""); err == nil {
		t.Fatal("expected error")
	}
	if err := FixtureID("ok-id_123"); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestLabelChecksVocabulary(t *testing.T) {
	if err := Label("a", []string{"a", "b"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := Label("c", []string{"a", "b"}); err == nil {
		t.Fatal("expected error for non-member label")
	}
	if err := Label("", []string{"a"}); err == nil {
		t.Fatal("expected error for empty label")
	}
	if err := Label("a", nil); err != nil {
		t.Fatalf("expected nil for nil vocab, got %v", err)
	}
}
