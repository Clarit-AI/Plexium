package pilot

import (
	"fmt"
	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
	"github.com/Clarit-AI/Plexium/evaluations/jev/scoring"
)

func Project(fixtures []protocol.Fixture, outcomes []Outcome, arm Arm) ([]scoring.Prediction, error) {
	gold := map[string]protocol.Fixture{}
	for _, f := range fixtures {
		gold[f.ID] = f
	}
	var preds []scoring.Prediction
	for _, o := range outcomes {
		if o.Slot.Arm != arm {
			continue
		}
		f, ok := gold[o.Slot.FixtureID]
		if !ok {
			return nil, fmt.Errorf("pilot: outcome fixture %s missing from local corpus", o.Slot.FixtureID)
		}
		p := scoring.Prediction{Source: scoring.Source(arm), FixtureID: f.ID, Task: f.Task, SourceGroup: f.SourceGroup, TemplateFamily: f.TemplateFamily, Split: f.Split, ExpectedLabel: f.ExpectedLabel, PredictedLabel: o.Label, Attempts: 1, ErrorMessage: o.Error}
		p.Abstained = o.Error == "" && (o.Label == "insufficient-evidence" || o.Label == "no-supported-relationship")
		preds = append(preds, p)
	}
	return preds, nil
}
