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
//
// Metrics with denominators that are not "all predictions" expose the
// numerator and denominator separately, plus an overall rate when the
// denominator differs. Gate-eligibility is a separate explicit flag so a
// corpus that does not exercise a label class is reported as
// "insufficient evidence" rather than silently passing or failing the gate.
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
//
// Probabilities and Confidence are also pointers / nil-maps. The protocol
// treats them as optional: the scorer never fabricates a zero value when the
// provider omits the field.
//
// When AttemptLatencies is set, it carries the per-attempt wall time in
// milliseconds; the first entry is the first request's duration and the
// remainder are subsequent retries. TotalWallMS, when set, is the wall time
// including body reads, backoff, and round-trip aggregation.
type Prediction struct {
	Source         Source
	FixtureID      string
	RunKey         string // optional repetition key for replay; empty = single run
	Task           protocol.Task
	SourceGroup    string
	TemplateFamily string // optional template family for independence checks
	Split          protocol.Split
	ExpectedLabel  string
	PredictedLabel string
	Abstained      bool
	// Optional probability map for Brier; nil/empty means "no distribution recorded".
	Probabilities map[string]float64
	// Confidence may be nil when the model/provider omitted it.
	Confidence *float64
	// Latency / Cost are pointers to indicate presence/absence.
	LatencyMS *float64
	CostUSD   *float64
	// Per-attempt durations (first request first). Nil means no breakdown.
	AttemptLatencies []float64
	// TotalWallMS is total wall time including backoff and body reads.
	TotalWallMS *float64
	Attempts    int
	// ErrorMessage, when non-empty, indicates an operational failure distinct
	// from a semantic abstention. Errors are counted as both failures and
	// missed positives; they are never a successful semantic abstention.
	ErrorMessage string
	// CandidateRecall is reported separately from classifier metrics.
	CandidateRecall *float64
}

// Report is the aggregated metrics over a slice of predictions.
type Report struct {
	Source                Source                                          `json:"source"`
	ProtocolVersion       string                                          `json:"protocolVersion"`
	FixtureCount          int                                             `json:"fixtureCount"`
	ByTask                map[protocol.Task]TaskReport                    `json:"byTask"`
	ByTaskSplit           map[protocol.Task]map[protocol.Split]TaskReport `json:"byTaskSplit"`
	OverallOverall        OverallReport                                   `json:"overall"`
	FailureCount          int                                             `json:"failureCount"`
	RetryCount            int                                             `json:"retryCount"`
	ConfidenceCalibration *CalibrationReport                              `json:"calibration,omitempty"`
}

// TaskReport contains metrics for one task over a fixed partition (either
// "all splits" or one specific split). The negative-class denominators
// drive the protocol's headline gates; the overall denominators are also
// reported so reviewers can see the contrast.
type TaskReport struct {
	FixtureCount         int                       `json:"fixtureCount"`
	Confusion            map[string]map[string]int `json:"confusion"`
	Labels               []string                  `json:"labels"`
	SupportedLabels      []string                  `json:"supportedLabels"`
	GateEligible         bool                      `json:"gateEligible"`
	GateIneligibleReason string                    `json:"gateIneligibleReason,omitempty"`
	MacroF1              float64                   `json:"macroF1"`
	MacroPrecision       float64                   `json:"macroPrecision"`
	MacroRecall          float64                   `json:"macroRecall"`
	Accuracy             float64                   `json:"accuracy"`
	AbstentionRate       float64                   `json:"abstentionRate"`
	AbstentionNum        int                       `json:"abstentionNumerator"`
	AbstentionDen        int                       `json:"abstentionDenominator"`
	UnsupportedAccept    *Rate                     `json:"unsupportedAcceptance,omitempty"`
	ContradictionMiss    *Rate                     `json:"contradictionMiss,omitempty"`
	EdgePrecision        float64                   `json:"edgePrecision"`
	EdgeRecall           float64                   `json:"edgeRecall"`
	DirectionAccuracy    float64                   `json:"directionAccuracy"`
	DirectionNum         int                       `json:"directionNumerator"`
	DirectionDen         int                       `json:"directionDenominator"`
	FailureCount         int                       `json:"failureCount"`
	CandidateRecallAvg   *float64                  `json:"candidateRecallAvg,omitempty"`
	// Support counts: number of cases whose gold is each label.
	LabelSupport map[string]int `json:"labelSupport"`
}

// Rate carries a numerator, denominator, and the derived value. When the
// denominator is zero, IsApplicable is false and the rate is reported as
// null in JSON.
type Rate struct {
	Numerator    int     `json:"numerator"`
	Denominator  int     `json:"denominator"`
	Rate         float64 `json:"rate"`
	IsApplicable bool    `json:"isApplicable"`
}

// OverallReport is the cross-task rollup.
type OverallReport struct {
	MacroF1        float64 `json:"macroF1"`
	Accuracy       float64 `json:"accuracy"`
	AbstentionRate float64 `json:"abstentionRate"`
}

// CalibrationReport holds reliability data over 5 equal-width bins. The
// scorer only fills it in when Probabilities are present. Absence is
// reported as "unscorable calibration" in the report metadata; the
// scorer never synthesizes a zero confidence to fill the bin.
type CalibrationReport struct {
	Bins             []CalibrationBin   `json:"bins"`
	BrierScore       float64            `json:"brierScore"`
	SampleCount      int                `json:"sampleCount"`
	CoverageByTh     map[string]float64 `json:"coverageByThreshold"`
	UnscorableReason string             `json:"unscorableReason,omitempty"`
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
// which pipeline produced each row. The protocolVersion string is the
// caller-supplied protocol version, used to label the report consistently
// with the manifest that produced it.
func Score(protocolVersion string, source Source, preds []Prediction) Report {
	rep := Report{
		Source:          source,
		ProtocolVersion: protocolVersion,
		FixtureCount:    len(preds),
		ByTask:          map[protocol.Task]TaskReport{},
		ByTaskSplit:     map[protocol.Task]map[protocol.Split]TaskReport{},
	}
	tasks := []protocol.Task{protocol.TaskEntityType, protocol.TaskRelationship, protocol.TaskClaimSupport}
	splits := []protocol.Split{protocol.SplitTuning, protocol.SplitHeldOut, protocol.SplitReserved}
	for _, t := range tasks {
		rep.ByTask[t] = scoreTask(protocolVersion, source, filterTask(preds, t), allowedLabelsForTask(t))
		rep.ByTaskSplit[t] = map[protocol.Split]TaskReport{}
		for _, s := range splits {
			subset := filterTaskAndSplit(preds, t, s)
			if len(subset) == 0 {
				continue
			}
			rep.ByTaskSplit[t][s] = scoreTask(protocolVersion, source, subset, allowedLabelsForTask(t))
		}
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

func filterTaskAndSplit(preds []Prediction, t protocol.Task, s protocol.Split) []Prediction {
	out := make([]Prediction, 0, len(preds))
	for _, p := range preds {
		if p.Task == t && p.Split == s {
			out = append(out, p)
		}
	}
	return out
}

// scoreTask computes per-task metrics. vocabulary is the full closed set
// for the task; supportedLabels is the subset that actually appears as gold
// in the predictions. Gate eligibility requires at least one positive
// example AND at least one negative example per the protocol's headline
// gates; the runner reports the explicit reason when eligibility fails.
func scoreTask(protocolVersion string, source Source, preds []Prediction, vocabulary []string) TaskReport {
	tr := TaskReport{
		FixtureCount: len(preds),
		Confusion:    map[string]map[string]int{},
		Labels:       append([]string{}, vocabulary...),
		LabelSupport: map[string]int{},
	}
	for _, l := range vocabulary {
		tr.Confusion[l] = map[string]int{}
	}
	// Reserve explicit outside-of-vocab buckets so writes cannot panic.
	tr.Confusion["__outside__"] = map[string]int{}
	for _, l := range vocabulary {
		tr.Confusion["__outside__"][l] = 0
	}
	if len(preds) == 0 {
		return tr
	}
	correct := 0
	failures := 0
	abstains := 0
	edgeTP := 0
	edgeFP := 0
	edgeFN := 0
	dirCorrect := 0
	dirTotal := 0
	candidateRecallSum := 0.0
	candidateRecallCount := 0
	unsupportedNum := 0
	unsupportedDen := 0
	contradictionMissNum := 0
	contradictionDen := 0
	for _, p := range preds {
		// Compute support counts from gold regardless of success.
		tr.LabelSupport[p.ExpectedLabel]++

		if p.ErrorMessage != "" {
			failures++
			// Errors count as missed positives where applicable.
			if p.Task == protocol.TaskClaimSupport && p.ExpectedLabel == "contradicted" {
				contradictionMissNum++
				contradictionDen++
			}
			if p.Task == protocol.TaskClaimSupport && p.ExpectedLabel == "supported" {
				// Error on a supported-expected case: not a contradiction miss.
			}
			if isAbstainExpected(p.Task, p.ExpectedLabel) && !p.Abstained {
				// not applicable; errors do not count as unsupported acceptance
			}
			if isAbstainExpected(p.Task, p.ExpectedLabel) && p.Abstained {
				// Errors during abstain-expected: the abstention is incidental,
				// not a correct decision. We do NOT count this as a successful
				// abstention.
			}
			continue
		}
		expected := p.ExpectedLabel
		predicted := p.PredictedLabel
		if !contains(vocabulary, expected) {
			tr.Confusion["__outside__"][predicted]++
			continue
		}
		if !contains(vocabulary, predicted) {
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
		// Unsupported-acceptance: harm is "pred a concrete predicate/label
		// for a case whose gold says abstain". Denominator is the count of
		// negative (abstain-expected) cases; numerator counts concrete
		// (non-abstain) predictions on those negatives.
		if isAbstainExpected(p.Task, expected) {
			unsupportedDen++
			if !p.Abstained && predicted != expected {
				unsupportedNum++
			}
		}
		// Contradiction-miss: cases whose gold is "contradicted".
		if p.Task == protocol.TaskClaimSupport && expected == "contradicted" {
			contradictionDen++
			if p.Abstained || predicted != "contradicted" {
				contradictionMissNum++
			}
		}
		// Direction / edge metrics for relationship tasks.
		if p.Task == protocol.TaskRelationship {
			if expected == predicted {
				edgeTP++
			} else if !isAbstainLabel(expected) && !isAbstainLabel(predicted) {
				edgeFP++
				edgeFN++
			} else if isAbstainLabel(expected) && !isAbstainLabel(predicted) {
				edgeFP++
			}
			if !isAbstainLabel(expected) && !isAbstainLabel(predicted) {
				dirTotal++
				if predicted == expected {
					dirCorrect++
				}
			}
		}
		if p.CandidateRecall != nil {
			candidateRecallSum += *p.CandidateRecall
			candidateRecallCount++
		}
	}
	tr.Accuracy = safeDiv(float64(correct), float64(len(preds)))
	tr.AbstentionNum = abstains
	tr.AbstentionDen = len(preds)
	tr.AbstentionRate = safeDiv(float64(abstains), float64(len(preds)))
	tr.UnsupportedAccept = buildRate(unsupportedNum, unsupportedDen)
	tr.ContradictionMiss = buildRate(contradictionMissNum, contradictionDen)
	tr.EdgePrecision = safeDiv(float64(edgeTP), float64(edgeTP+edgeFP))
	tr.EdgeRecall = safeDiv(float64(edgeTP), float64(edgeTP+edgeFN))
	if dirTotal > 0 {
		tr.DirectionNum = dirCorrect
		tr.DirectionDen = dirTotal
		tr.DirectionAccuracy = float64(dirCorrect) / float64(dirTotal)
	}
	if candidateRecallCount > 0 {
		avg := candidateRecallSum / float64(candidateRecallCount)
		tr.CandidateRecallAvg = &avg
	}
	tr.FailureCount = failures
	// Compute supported labels (gold-observed).
	supported := make([]string, 0, len(vocabulary))
	for _, l := range vocabulary {
		if tr.LabelSupport[l] > 0 {
			supported = append(supported, l)
		}
	}
	tr.SupportedLabels = supported
	// Gate eligibility: macro-F1 ≥ 0.90 only meaningful if every vocabulary
	// label has at least one gold-observed case. Unsupported-acceptance
	// gate requires at least one abstain-expected case. Contradiction
	// recall requires at least one contradicted case.
	tr.GateEligible, tr.GateIneligibleReason = gateEligibility(taskOf(preds), tr)
	tr.MacroF1, tr.MacroPrecision, tr.MacroRecall = macroF1(tr.Confusion, vocabulary)
	return tr
}

// taskOf returns the task the supplied predictions share. The runner
// always filters by task before calling scoreTask, so this is safe.
func taskOf(preds []Prediction) protocol.Task {
	if len(preds) == 0 {
		return ""
	}
	return preds[0].Task
}

// gateEligibility returns (eligible, reason). The protocol gates are not
// meaningful unless the corpus exercises every label in the closed
// vocabulary (macro-F1) and at least one negative per negative-class gate.
// The task is passed in so this can run on per-split sub-reports too.
func gateEligibility(task protocol.Task, tr TaskReport) (bool, string) {
	if len(tr.Labels) == 0 {
		return false, "task has no vocabulary"
	}
	missing := []string{}
	for _, l := range tr.Labels {
		if tr.LabelSupport[l] == 0 {
			missing = append(missing, l)
		}
	}
	if len(missing) > 0 {
		return false, "labels not exercised by gold: " + strings.Join(missing, ",")
	}
	if task == protocol.TaskRelationship || task == protocol.TaskClaimSupport {
		// Negative-class gate requires at least one abstain-expected case.
		if tr.UnsupportedAccept != nil && !tr.UnsupportedAccept.IsApplicable {
			return false, "no abstain-expected negative cases for unsupported-acceptance gate"
		}
	}
	if task == protocol.TaskClaimSupport {
		// Contradiction-recall gate requires at least one contradicted case.
		if tr.ContradictionMiss != nil && !tr.ContradictionMiss.IsApplicable {
			return false, "no contradicted cases for contradiction-recall gate"
		}
	}
	return true, ""
}

func buildRate(num, den int) *Rate {
	r := &Rate{Numerator: num, Denominator: den}
	if den > 0 {
		r.IsApplicable = true
		r.Rate = float64(num) / float64(den)
	}
	return r
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
// matrix indexed by label. The implementation counts outside-vocab cells
// separately so the macro average is taken over the closed vocabulary only.
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
		return append([]string{}, protocol.AllowedLabelsFor(t)...)
	case protocol.TaskClaimSupport:
		return protocol.AllowedLabelsFor(t)
	}
	return nil
}

func isAbstainLabel(label string) bool {
	l := strings.ToLower(strings.TrimSpace(label))
	return l == "insufficient-evidence" || l == "no-supported-relationship"
}

// isAbstainExpected reports whether the supplied gold label is one of the
// abstain-class labels for the task.
func isAbstainExpected(t protocol.Task, label string) bool {
	if !isAbstainLabel(label) {
		return false
	}
	// The abstain labels live in different vocabularies per task; we still
	// return true when label is in the abstain set, but the scorer only
	// counts them where the task vocabulary includes them.
	switch t {
	case protocol.TaskRelationship, protocol.TaskClaimSupport:
		return true
	}
	return false
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// computeCalibration returns a calibration report only when Probabilities
// (or Confidence) are recorded. When the corpus has *no* probabilities
// recorded (the typical baseline case), the report is nil — the JSON
// layer emits a "calibration: null" and a metadata note that calibration
// is unscorable, not absent. The scorer never synthesizes zero confidence.
func computeCalibration(preds []Prediction) *CalibrationReport {
	scored := make([]Prediction, 0, len(preds))
	for _, p := range preds {
		if p.Confidence != nil || len(p.Probabilities) > 0 {
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
		CoverageByTh: map[string]float64{},
	}
	sumSquared := 0.0
	for _, p := range scored {
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
		// Probability-based confidence when Confidence is nil but the
		// distribution exists.
		conf := 0.0
		if p.Confidence != nil {
			conf = *p.Confidence
		} else if maxProb, ok := maxProb(p.Probabilities); ok {
			conf = maxProb
		} else {
			// Calibration is unscorable: missing confidence AND missing
			// probability map cannot be synthesized; report a strict reason.
			rep.UnscorableReason = "missing confidence; probabilities present without max"
			continue
		}
		conf = clamp(conf, 0, 1)
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
	thresholds := []float64{0, 0.5, 0.7, 0.85, 0.95}
	for _, th := range thresholds {
		covered := 0
		for _, p := range scored {
			conf := 0.0
			if p.Confidence != nil {
				conf = *p.Confidence
			} else if maxProb, ok := maxProb(p.Probabilities); ok {
				conf = maxProb
			} else {
				continue
			}
			if conf >= th {
				covered++
			}
		}
		rep.CoverageByTh[formatThreshold(th)] = float64(covered) / float64(len(scored))
	}
	rep.SampleCount = len(scored)
	return rep
}

func maxProb(p map[string]float64) (float64, bool) {
	if len(p) == 0 {
		return 0, false
	}
	max := -1.0
	for _, v := range p {
		if v > max {
			max = v
		}
	}
	return max, true
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
