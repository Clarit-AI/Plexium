package pilot

import (
	"fmt"
	"math/big"

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
		attempts := 0
		if o.RequestSent {
			attempts = 1
		}
		p := scoring.Prediction{Source: scoring.Source(arm), FixtureID: f.ID, Task: f.Task, SourceGroup: f.SourceGroup, TemplateFamily: f.TemplateFamily, Split: f.Split, ExpectedLabel: f.ExpectedLabel, PredictedLabel: o.Label, Attempts: attempts, ErrorMessage: o.Error}
		if o.Observation != nil {
			p.Confidence = o.Observation.Confidence
			if o.Observation.Probabilities != nil {
				p.Probabilities = make(map[string]float64, len(o.Observation.Probabilities))
				for k, v := range o.Observation.Probabilities {
					p.Probabilities[k] = v
				}
			}
			if o.RequestSent {
				ms := float64(o.Observation.DurationNanos) / float64(1e6)
				p.LatencyMS = &ms
				p.TotalWallMS = &ms
				p.AttemptLatencies = []float64{ms}
			}
			cost := o.Observation.Billing.Cost
			if cost.Present && !cost.Null && cost.Valid {
				if rat, ok := new(big.Rat).SetString(cost.Raw); ok {
					value, _ := rat.Float64()
					p.CostUSD = &value
				}
			}
		}
		p.Abstained = o.Error == "" && (o.Label == "insufficient-evidence" || o.Label == "no-supported-relationship")
		preds = append(preds, p)
	}
	return preds, nil
}
