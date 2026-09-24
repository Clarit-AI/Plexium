package pilot

// Decision 5 (KHAEntertainment, 2026-09-23): the KNOWN B1 discrepancy class
// (account charge vs cost_details.upstream_inference_cost at the documented
// ~0.99 ratio) is tolerated with conservative accounting; every other
// discrepancy or novel anomaly keeps the Decision-2 halt-both-arms net.
//
// decision5Text is the verbatim Decision-5 text extracted from
// artifacts/jev-policy-decisions/index.md; its SHA-256 is bound into every
// halt-supersession record so the adjudicated wording is auditable.

import (
	"math/big"
	"strings"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
)

const decision5Text = "## Decision 5 \u2014 known discrepancy class tolerance (2026-09-23)\n\nDecided by **KHAEntertainment** after the Decision-2 halt net fired on the pilot's first Nano call (run state: 2 of 48 calls completed, ~115 \u00b5$ settled \u2014 Jev's first call reconciled cleanly at 60.06 \u00b5$ as billed; Nano showed the documented ~0.99 `cost` vs `cost_details.upstream_inference_cost` ratio and both arms halted).\n\n- **The known class is tolerated, narrowly defined**: a positive-billing discrepancy where `cost` and `cost_details.upstream_inference_cost` are both present and positive and their ratio lies within the documented ~0.99 envelope (the implementing round pins the exact bounds from the evidence and binds the definition into the report basis). This class is **accounted conservatively \u2014 always the higher of the two figures** \u2014 and does NOT halt.\n- **The net is preserved for everything else**: any discrepancy outside the class (missing counterpart figure, non-positive figures, unbounded ratio, magnitude shock) or any novel anomaly still **halts both arms immediately** (Decision 2 otherwise unchanged).\n- **Auditable resume only**: the durable halt event is never deleted or rewritten; an explicit append-only superseding record binds this decision, the tolerance rule, and the halted event identity. The run resumes under the SAME allocation identity and run identity (exact record equality), settled attempts are never resent (no-uncertain-resend preserved), and any conservative top-up of the halted attempt's settlement is an append-only adjustment, never a rewrite.\n- **Reports keep every truthful statement**: `rateSemantics: UNRECONCILED`, billing-basis `liveContractsVerified` not-true, per-attempt conservative max accounting disclosed, and this tolerance rule stated explicitly.\n- Remaining exposure: 46 calls (~15,433 \u00b5$ reservations) within the 949,856 \u00b5$ CombinedCap.\n\nRationale: the user's original \"run under conservative non-tariff accounting bounds\" intent is the faithful reading \u2014 the halt net was designed for unknown/unbounded anomalies, and the systematic documented class is managed by conservative accounting plus full disclosure."

// RateSemanticsTolerance is a pinned tolerance rule for the known B1
// discrepancy class. The ratio bounds are inclusive on exact decimal
// arithmetic (reported/upstream).
type RateSemanticsTolerance struct {
	RuleName          string
	DecisionSHA256    string
	DecisionReference string
	RatioMin          string // inclusive lower bound on cost / upstream_inference_cost
	RatioMax          string // inclusive upper bound
	Description       string
}

// Decision5Tolerance returns the pinned Decision-5 rule. The documented
// observations are exactly 0.99 (three independent figure pairs:
// 8613/8700, 56529/57100, 54549/55100). The pinned envelope [0.985, 0.995]
// admits the documented 1% reduction with +/-0.005 slack for nine-decimal
// figure rounding, and rejects any deeper reduction, any markup
// (ratio > 1), any unbounded/absurd ratio, and any novel anomaly.
func Decision5Tolerance() RateSemanticsTolerance {
	return RateSemanticsTolerance{
		RuleName:          "decision-5-known-b1-class-v1",
		DecisionSHA256:    decision5Digest(),
		DecisionReference: "artifacts/jev-policy-decisions Decision 5 (2026-09-23)",
		RatioMin:          "0.985",
		RatioMax:          "0.995",
		Description: "decision-5-known-b1-class-v1: a positive-billing discrepancy is tolerated iff " +
			"(1) cost and cost_details.upstream_inference_cost are both present, valid, and strictly positive; " +
			"(2) the exact ratio cost/upstream_inference_cost lies within the pinned envelope [0.985, 0.995] " +
			"(documented observations are exactly 0.99; slack covers nine-decimal rounding only); and " +
			"(3) the conservative figure max(cost, upstream_inference_cost) does not exceed the slot's " +
			"reserved conservative bound (no magnitude shock). Within the class: account conservatively at " +
			"max(reported, upstream) exactly once, record the discrepancy on the outcome row, and continue. " +
			"Outside the class (missing counterpart, non-positive figures, ratio outside the envelope, " +
			"magnitude shock) or any novel anomaly: the Decision-2 halt-both-arms path is unchanged.",
	}
}

// Decision5Digest is the SHA-256 of the verbatim Decision-5 text.
func Decision5Digest() string { return decision5Digest() }

func decision5Digest() string { return hashBytes([]byte(decision5Text)) }

// RateToleranceVerdict reports whether a billing discrepancy belongs to the
// rule's tolerated class, and the conservative accounted figure.
type RateToleranceVerdict struct {
	Tolerated       bool
	Conservative    ledger.MicroUnit // max(reported, upstream) in integer microdollars
	ConservativeRaw string           // the winning figure's raw lexeme, verbatim
	Detail          string           // recordable classification note (figures verbatim)
}

// rateToleranceVerdict classifies one billing observation under a pinned rule.
func rateToleranceVerdict(b adapter.BillingObservation, reservation ledger.MicroUnit, rule RateSemanticsTolerance) RateToleranceVerdict {
	if b.RateSemanticsDiscrepancy == "" {
		return RateToleranceVerdict{} // no discrepancy: nothing to tolerate
	}
	rep, repOK := positiveRat(b.Cost)
	up, upOK := positiveRat(b.CostDetails.UpstreamInferenceCost)
	if !repOK || !upOK {
		return RateToleranceVerdict{} // missing counterpart or non-positive figure
	}
	min, ok1 := new(big.Rat).SetString(rule.RatioMin)
	max, ok2 := new(big.Rat).SetString(rule.RatioMax)
	if !ok1 || !ok2 || min == nil || max == nil {
		return RateToleranceVerdict{}
	}
	ratio := new(big.Rat).Quo(rep, up)
	if ratio.Cmp(min) < 0 || ratio.Cmp(max) > 0 {
		return RateToleranceVerdict{} // ratio outside the envelope (incl. absurd ratios)
	}
	conservativeRaw := b.Cost.Raw
	conservative, err := decimalMicrodollars(b.Cost.Raw)
	if err != nil {
		return RateToleranceVerdict{}
	}
	if up.Cmp(rep) >= 0 {
		conservativeRaw = b.CostDetails.UpstreamInferenceCost.Raw
		conservative, err = decimalMicrodollars(b.CostDetails.UpstreamInferenceCost.Raw)
		if err != nil {
			return RateToleranceVerdict{}
		}
	}
	if conservative > reservation {
		return RateToleranceVerdict{} // magnitude shock: outside the manifested per-attempt envelope
	}
	detail := "decision-5 known B1 class tolerated: cost " + b.Cost.Raw +
		" vs upstream_inference_cost " + b.CostDetails.UpstreamInferenceCost.Raw +
		" (ratio " + ratio.FloatString(6) + " within [" + rule.RatioMin + "," + rule.RatioMax + "])" +
		"; accounted conservatively at max figure " + conservativeRaw
	return RateToleranceVerdict{Tolerated: true, Conservative: conservative, ConservativeRaw: conservativeRaw, Detail: detail}
}

// conservativeMaxMicrodollars returns max(reported, upstream) for an already
// tolerated row (used at replay so report rows account exactly what the
// tolerance rule accounts, under whatever rule admitted the row).
func conservativeMaxMicrodollars(b adapter.BillingObservation) ledger.MicroUnit {
	rep, repOK := positiveRat(b.Cost)
	up, upOK := positiveRat(b.CostDetails.UpstreamInferenceCost)
	cost, err := decimalMicrodollars(b.Cost.Raw)
	if err != nil {
		return 0
	}
	if repOK && upOK && up.Cmp(rep) >= 0 {
		if upCost, err := decimalMicrodollars(b.CostDetails.UpstreamInferenceCost.Raw); err == nil && upCost > cost {
			return upCost
		}
	}
	return cost
}

func positiveRat(f adapter.DecimalField) (*big.Rat, bool) {
	if !f.Present || f.Null || !f.Valid || strings.TrimSpace(f.Raw) == "" {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(f.Raw)
	if !ok || r.Sign() <= 0 {
		return nil, false
	}
	return r, true
}
