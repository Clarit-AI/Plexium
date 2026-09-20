package baseline

import (
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

func TestPredictEntityTypeAlwaysDocument(t *testing.T) {
	got, abstained := Predict(protocol.TaskEntityType, Wrap(nil))
	if got != "document" {
		t.Fatalf("expected document, got %q", got)
	}
	if abstained {
		t.Fatalf("entity-type baseline should not be an abstention")
	}
}

func TestPredictRelationshipAlwaysInsufficientEvidence(t *testing.T) {
	got, abstained := Predict(protocol.TaskRelationship, Wrap([]protocol.Candidate{{ID: "title:0", Title: "X", Role: "title"}}))
	if got != "insufficient-evidence" {
		t.Fatalf("expected insufficient-evidence, got %q", got)
	}
	if !abstained {
		t.Fatalf("relationship baseline should be an abstention")
	}
}

func TestPredictClaimSupportInsufficientEvidence(t *testing.T) {
	got, abstained := Predict(protocol.TaskClaimSupport, Wrap(nil))
	if got != "insufficient-evidence" {
		t.Fatalf("expected insufficient-evidence, got %q", got)
	}
	if !abstained {
		t.Fatalf("claim-support baseline should abstain")
	}
}

func TestPredictUnknownTaskAbstains(t *testing.T) {
	got, abstained := Predict(protocol.Task("mystery"), Wrap(nil))
	if got != "insufficient-evidence" {
		t.Fatalf("expected insufficient-evidence, got %q", got)
	}
	if !abstained {
		t.Fatalf("unknown task should abstain")
	}
}

// v0.4 baselines: behavioral regression for the user-selected
// convention (document typing by primary subject, abstain on mixed
// subjects or missing evidence). PredictV2 must explicitly abstain
// for entity-type and candidate-type; Predict remains as legacy
// default-fallback for backward comparison.
func TestPredictV2EntityTypeAbstains(t *testing.T) {
	for _, list := range []Shortlist{nil, Wrap(nil), Wrap([]protocol.Candidate{{ID: "x"}})} {
		label, abstained := PredictV2(protocol.TaskEntityType, list)
		if label != "insufficient-evidence" {
			t.Errorf("PredictV2(entity-type, %v) = %q, want insufficient-evidence", list, label)
		}
		if !abstained {
			t.Errorf("PredictV2(entity-type, %v) abstained=false, want true", list)
		}
	}
}

func TestPredictV2CandidateTypeAbstains(t *testing.T) {
	for _, list := range []Shortlist{nil, Wrap(nil), Wrap([]protocol.Candidate{{ID: "x"}}), Wrap([]protocol.Candidate{{ID: "x"}, {ID: "y"}})} {
		label, abstained := PredictV2(protocol.TaskCandidateType, list)
		if label != "insufficient-evidence" {
			t.Errorf("PredictV2(candidate-type, %v) = %q, want insufficient-evidence", list, label)
		}
		if !abstained {
			t.Errorf("PredictV2(candidate-type, %v) abstained=false, want true", list)
		}
	}
}

func TestPredictV2RelationshipAndClaimSupportUnchanged(t *testing.T) {
	if label, abstained := PredictV2(protocol.TaskRelationship, Wrap(nil)); label != "insufficient-evidence" || !abstained {
		t.Errorf("PredictV2(relationship) = (%q,%v), want (insufficient-evidence,true)", label, abstained)
	}
	if label, abstained := PredictV2(protocol.TaskClaimSupport, Wrap(nil)); label != "insufficient-evidence" || !abstained {
		t.Errorf("PredictV2(claim-support) = (%q,%v), want (insufficient-evidence,true)", label, abstained)
	}
}

func TestLegacyPredictStillDefaultFallback(t *testing.T) {
	// Predict (legacy v0.3) must NOT change behaviour: entity-type
	// returns "document" as default fallback; candidate-type returns
	// "CONCEPT". These are the v0.3 default-fallback outputs.
	if label, abstained := Predict(protocol.TaskEntityType, Wrap(nil)); label != "document" || abstained {
		t.Errorf("Predict(entity-type) = (%q,%v), want (document,false)", label, abstained)
	}
	if label, abstained := Predict(protocol.TaskCandidateType, Wrap(nil)); label != "CONCEPT" || abstained {
		t.Errorf("Predict(candidate-type) = (%q,%v), want (CONCEPT,false)", label, abstained)
	}
}

// ProtocolVersion constant is bumped to v0.4.0 — protocol package test
// will fail if reverted. The behavioural regression below checks the
// vocabulary includes insufficient-evidence for both typing tasks.
func TestV0D4VocabularyIncludesInsufficientEvidence(t *testing.T) {
	if got := protocol.DocumentTypeLabels; !contains(got, "insufficient-evidence") {
		t.Errorf("DocumentTypeLabels missing insufficient-evidence: %v", got)
	}
	if got := protocol.CandidateTypeLabels; !contains(got, "insufficient-evidence") {
		t.Errorf("CandidateTypeLabels missing insufficient-evidence: %v", got)
	}
	if protocol.ProtocolVersion != "0.4.0" {
		t.Errorf("ProtocolVersion = %q, want 0.4.0", protocol.ProtocolVersion)
	}
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}
