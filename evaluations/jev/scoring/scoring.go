// Package scoring implements the per-task metrics the protocol v0.1
// requires. The scorer is offline-only: it consumes a list of
// Prediction records (one per fixture run) plus optional probability and
// candidate-recall data, and emits confusion matrices, macro-F1, edge
// precision/recall, direction accuracy, abstention/coverage, Brier and
// reliability, and per-task breakdowns.
//
// Crucially, the scorer never invents latency, cost, or confidence for the
// deterministic baseline; runs that did not record those fields simply lack
// them in the report.
package scoring

import (
	"strconv"
	"strings"

	"github.com/Clarit-AI/Plexium/evaluations/jev/protocol"
)

// Source identifies which pipeline produced a Prediction.
type Source string

const (
	SourceBaseline  Source = "deterministic-baseline"
	SourceReplay    Source = "jsonl-replay"
	SourceDecisions Source = "decisions"
	SourceChat      Source = "chat"
)

// Prediction is one fixture/observation pair recorded by a runner. Latency
// and Cost are pointers so the scorer can tell whether they were observed
// (and may report them) versus absent (and must NOT fabricate).
type Prediction struct {
	Source         Source
	FixtureID      string
	Task           protocol.Task
	SourceGroup    string
	Split          protocol.Split
	ExpectedLabel  string
	PredictedLabel string
	Abstained      bool
	// Optional probability map for Brier; nil means "no distribution recorded".
	Probabilities map[string]float64
	Confidence    *float64
	// Latency / Cost are pointers to indicate presence/absence.
	LatencyMS *float64
	CostUSD   *float64
	Attempts  int
	// ErrorMessage, when non-empty, indicates an operational failure distinct
	// from a semantic abstention.
	ErrorMessage string
	// CandidateRecall is reported separately from classifier metrics.
	CandidateRecall *float64
}

// Report is the aggregated metrics over a slice of predictions.
type Report struct {
	Source                Source                       `json:"source"`
	FixtureCount          int                          `json:"fixtureCount"`
	ByTask                map[protocol.Task]TaskReport `json:"byTask"`
	OverallOverall        OverallReport                `json:"overall"`
	FailureCount          int                          `json:"failureCount"`
	RetryCount            int                          `json:"retryCount"`
	ConfidenceCalibration *CalibrationReport           `json:"calibration,omitempty"`
}

// TaskReport contains metrics for one task.
type TaskReport struct {
	FixtureCount       int                       `json:"fixtureCount"`
	Confusion          map[string]map[string]int `json:"confusion"`
	Labels             []string                  `json:"labels"`
	MacroF1            float64                   `json:"macroF1"`
	MacroPrecision     float64                   `json:"macroPrecision"`
	MacroRecall        float64                   `json:"macroRecall"`
	Accuracy           float64                   `json:"accuracy"`
	AbstentionRate     float64                   `json:"abstentionRate"`
	UnsupportedAccept  float64                   `json:"unsupportedAcceptance"`
	ContradictionMiss  float64                   `json:"contradictionMiss"`
	EdgePrecision      float64                   `json:"edgePrecision"`
	EdgeRecall         float64                   `json:"edgeRecall"`
	DirectionAccuracy  float64                   `json:"directionAccuracy"`
	CandidateRecallAvg *float64                  `json:"candidateRecallAvg,omitempty"`
	FailureCount       int                       `json:"failureCount"`
}

// OverallReport is the cross-task rollup.
type OverallReport struct {
	MacroF1        float64 `json:"macroF1"`
	Accuracy       float64 `json:"accuracy"`
	AbstentionRate float64 `json:"abstentionRate"`
}

// CalibrationReport holds reliability data over 5 equal-width bins. The
// scorer only fills it in when Probabilities are present.
type CalibrationReport struct {
	Bins         []CalibrationBin   `json:"bins"`
	BrierScore   float64            `json:"brierScore"`
	SampleCount  int                `json:"sampleCount"`
	CoverageByTh map[string]float64 `json:"coverageByThreshold"`
}

// CalibrationBin is one bin of the reliability histogram.
type CalibrationBin struct {
	Lower         float64 `json:"lower"`
	Upper         float64 `json:"upper"`
	Count         int     `json:"count"`
	AvgConfidence float64 `json:"avgConfidence"`
	Accuracy      float64 `json:"accuracy"`
}

// Score produces a Report from the supplied predictions grouped by task. The
// source label is recorded verbatim on the Report so the operator can tell
// which pipeline produced each row.
func Score(source Source, preds []Prediction) Report {
	rep := Report{Source: source, FixtureCount: len(preds), ByTask: map[protocol.Task]TaskReport{}}
	for _, t := range []protocol.Task{protocol.TaskEntityType, protocol.TaskRelationship, protocol.TaskClaimSupport} {
		rep.ByTask[t] = scoreTask(filterTask(preds, t))
	}
	rep.OverallOverall = rollup(rep.ByTask)
	rep.FailureCount = countFailures(preds)
	rep.RetryCount = countRetries(preds)
	if cal := computeCalibration(preds); cal != nil {
		rep.ConfidenceCalibration = cal
	}
	return rep
}

func filterTask(preds []Prediction, t protocol.Task) []Prediction {
	out := make([]Prediction, 0, len(preds))
	for _, p := range preds {
		if p.Task == t {
			out = append(out, p)
		}
	}
	return out
}

func scoreTask(preds []Prediction) TaskReport {
	tr := TaskReport{
		FixtureCount: len(preds),
		Confusion:    map[string]map[string]int{},
	}
	if len(preds) == 0 {
		return tr
	}
	labels := allowedLabelsForTask(preds[0].Task)
	tr.Labels = append([]string{}, labels...)
	for _, l := range labels {
		tr.Confusion[l] = map[string]int{}
	}
	correct := 0
	failures := 0
	abstains := 0
	unsupportedAccepted := 0
	contradictionMissed := 0
	edgeTP := 0
	edgeFP := 0
	edgeFN := 0
	dirCorrect := 0
	dirTotal := 0
	candidateRecallSum := 0.0
	candidateRecallCount := 0
	for _, p := range preds {
		if p.ErrorMessage != "" {
			failures++
			continue
		}
		expected := p.ExpectedLabel
		predicted := p.PredictedLabel
		if !contains(labels, expected) {
			tr.Confusion["__outside__"][predicted]++
			continue
		}
		if !contains(labels, predicted) {
			tr.Confusion[expected]["__outside__"]++
			continue
		}
		tr.Confusion[expected][predicted]++
		if predicted == expected {
			correct++
		}
		if p.Abstained {
			abstains++
		}
		// Unsupported acceptance: predicted a concrete predicate/label for a
		// case whose expected answer is insufficient/no-support.
		if isAbstainLabel(expected) && !p.Abstained && predicted != expected {
			unsupportedAccepted++
		}
		// Contradiction miss: claim-support case expected "contradicted" but
		// the model abstained or returned a non-contradicted label.
		if p.Task == protocol.TaskClaimSupport && expected == "contradicted" && predicted != "contradicted" {
			contradictionMissed++
		}
		// Direction / edge metrics for relationship tasks.
		if p.Task == protocol.TaskRelationship {
			if expected == predicted {
				edgeTP++
			} else if !isAbstainLabel(expected) && !isAbstainLabel(predicted) {
				// Concrete but wrong predicate.
				edgeFP++
				edgeFN++
			} else if isAbstainLabel(expected) && !isAbstainLabel(predicted) {
				edgeFP++
			}
			// Direction accuracy: any concrete predicted predicate that
			// matches the expected predicate regardless of direction
			// contributes to direction accuracy if the source/target IDs
			// appear. Without IDs we approximate via label equality. This
			// metric is only meaningful when the runner supplies direction
			// info; the helper below returns "label-only" precision.
			if predicted == expected {
				dirCorrect++
			}
			dirTotal++
		}
		if p.CandidateRecall != nil {
			candidateRecallSum += *p.CandidateRecall
			candidateRecallCount++
		}
	}
	tr.Accuracy = float64(correct) / float64(len(preds))
	tr.AbstentionRate = float64(abstains) / float64(len(preds))
	tr.UnsupportedAccept = float64(unsupportedAccepted) / float64(len(preds))
	tr.contradictionMissRate(float64(contradictionMissed), len(preds))
	tr.EdgePrecision = safeDiv(float64(edgeTP), float64(edgeTP+edgeFP))
	tr.EdgeRecall = safeDiv(float64(edgeTP), float64(edgeTP+edgeFN))
	if dirTotal > 0 {
		tr.DirectionAccuracy = float64(dirCorrect) / float64(dirTotal)
	}
	if candidateRecallCount > 0 {
		avg := candidateRecallSum / float64(candidateRecallCount)
		tr.CandidateRecallAvg = &avg
	}
	tr.FailureCount = failures
	tr.MacroF1, tr.MacroPrecision, tr.MacroRecall = macroF1(tr.Confusion, labels)
	return tr
}

func (tr *TaskReport) contradictionMissRate(missed float64, total int) {
	if total == 0 {
		return
	}
	tr.ContradictionMiss = missed / float64(total)
}

func rollup(byTask map[protocol.Task]TaskReport) OverallReport {
	var sumF1, sumAcc, sumAbst float64
	n := 0
	for _, t := range []protocol.Task{protocol.TaskEntityType, protocol.TaskRelationship, protocol.TaskClaimSupport} {
		tr := byTask[t]
		if tr.FixtureCount == 0 {
			continue
		}
		sumF1 += tr.MacroF1
		sumAcc += tr.Accuracy
		sumAbst += tr.AbstentionRate
		n++
	}
	if n == 0 {
		return OverallReport{}
	}
	return OverallReport{
		MacroF1:        sumF1 / float64(n),
		Accuracy:       sumAcc / float64(n),
		AbstentionRate: sumAbst / float64(n),
	}
}

func countFailures(preds []Prediction) int {
	n := 0
	for _, p := range preds {
		if p.ErrorMessage != "" {
			n++
		}
	}
	return n
}

func countRetries(preds []Prediction) int {
	n := 0
	for _, p := range preds {
		if p.Attempts > 1 {
			n++
		}
	}
	return n
}

// macroF1 computes macro-averaged precision, recall, and F1 from a confusion
// matrix indexed by label.
func macroF1(conf map[string]map[string]int, labels []string) (f1, prec, rec float64) {
	if len(labels) == 0 {
		return 0, 0, 0
	}
	sumF1 := 0.0
	sumP := 0.0
	sumR := 0.0
	for _, l := range labels {
		tp := conf[l][l]
		fp := 0
		fn := 0
		for _, other := range labels {
			if other == l {
				continue
			}
			fp += conf[other][l]
			fn += conf[l][other]
		}
		p := safeDiv(float64(tp), float64(tp+fp))
		r := safeDiv(float64(tp), float64(tp+fn))
		f := 0.0
		if p+r > 0 {
			f = 2 * p * r / (p + r)
		}
		sumF1 += f
		sumP += p
		sumR += r
	}
	n := float64(len(labels))
	return sumF1 / n, sumP / n, sumR / n
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func allowedLabelsForTask(t protocol.Task) []string {
	switch t {
	case protocol.TaskEntityType:
		return protocol.AllowedLabelsFor(t)
	case protocol.TaskRelationship:
		return append(append([]string{}, protocol.AllowedLabelsFor(t)...), "no-supported-relationship", "insufficient-evidence")
	case protocol.TaskClaimSupport:
		return protocol.AllowedLabelsFor(t)
	}
	return nil
}

func isAbstainLabel(label string) bool {
	l := strings.ToLower(strings.TrimSpace(label))
	return l == "insufficient-evidence" || l == "no-supported-relationship"
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

func computeCalibration(preds []Prediction) *CalibrationReport {
	scored := make([]Prediction, 0, len(preds))
	for _, p := range preds {
		if p.Confidence != nil && len(p.Probabilities) > 0 {
			scored = append(scored, p)
		}
	}
	if len(scored) == 0 {
		return nil
	}
	rep := &CalibrationReport{
		Bins: []CalibrationBin{
			{Lower: 0.0, Upper: 0.2},
			{Lower: 0.2, Upper: 0.4},
			{Lower: 0.4, Upper: 0.6},
			{Lower: 0.6, Upper: 0.8},
			{Lower: 0.8, Upper: 1.0 + 1e-9},
		},
		SampleCount:  len(scored),
		CoverageByTh: map[string]float64{},
	}
	sumSquared := 0.0
	for _, p := range scored {
		// Multiclass Brier: sum over labels of (predicted - actual)^2.
		expected := p.ExpectedLabel
		for label, prob := range p.Probabilities {
			actual := 0.0
			if label == expected {
				actual = 1.0
			}
			diff := prob - actual
			sumSquared += diff * diff
		}
	}
	rep.BrierScore = sumSquared / float64(len(scored))
	for _, p := range scored {
		conf := clamp(*p.Confidence, 0, 1)
		correct := 0
		if p.PredictedLabel == p.ExpectedLabel {
			correct = 1
		}
		for i := range rep.Bins {
			b := &rep.Bins[i]
			if conf >= b.Lower && conf < b.Upper {
				b.Count++
				b.AvgConfidence += conf
				b.Accuracy += float64(correct)
				break
			}
		}
	}
	for i := range rep.Bins {
		b := &rep.Bins[i]
		if b.Count == 0 {
			continue
		}
		b.AvgConfidence /= float64(b.Count)
		b.Accuracy /= float64(b.Count)
	}
	// Coverage at thresholds.
	thresholds := []float64{0, 0.5, 0.7, 0.85, 0.95}
	for _, th := range thresholds {
		covered := 0
		for _, p := range scored {
			if *p.Confidence >= th {
				covered++
			}
		}
		rep.CoverageByTh[formatThreshold(th)] = float64(covered) / float64(len(scored))
	}
	return rep
}

func formatThreshold(th float64) string {
	if th == 0 {
		return "0"
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(th, 'f', -1, 64), "0"), ".")
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
