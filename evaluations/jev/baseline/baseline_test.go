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
