package pilot

// Decision 6 (KHAEntertainment, 2026-09-23): probability-mass admission — a
// Choice distribution whose exact sum S satisfies |S - 1| <= 0.02 is
// ADMITTED with the deficit recorded as a calibration signal; scores are
// never renormalized. Outside that class (or on any structural failure) the
// existing not-admitted -> billing-invalid -> halt-both-arms path is
// unchanged.
//
// decision6Text is the verbatim Decision-6 text extracted from
// artifacts/jev-policy-decisions/index.md; its SHA-256 is bound into every
// Decision-6 halt-supersession record.

const decision6Text = "## Decision 6 \u2014 probability-mass admission rule (2026-09-23)\n\nDecided by **KHAEntertainment** after the run halted at 25/48 calls on a Jev response whose probability scores summed to 0.99 (schema tolerance 0.001 \u2192 response not admitted \u2192 billing invalid \u2192 both arms halted).\n\n- **Admit with deficit recorded**: a response whose probability scores sum to S with |S \u2212 1| \u2264 0.02 is ADMITTED. Scores are preserved byte-verbatim (numeric fidelity); the exact sum S and deficit (S \u2212 1) are recorded on the outcome row; a deficit marker is set whenever |S \u2212 1| > 0.001. Decisions are scored normally (argmax/abstain per rubric); the deficit is a CALIBRATION signal, and a contract-violation count appears in the report (feeds the maintenance-complexity measurement). Scores are NOT renormalized \u2014 renormalization would erase the signal.\n- **The net is preserved outside the class**: |S \u2212 1| > 0.02 or any structural failure (missing/invalid fields, non-finite values) keeps the existing not-admitted \u2192 billing-invalid \u2192 halt-both-arms path unchanged.\n- **Standing rule** for all remaining attempts in this run: mass-deficit admissions never halt.\n- **Auditable second supersession**: the halt at journal sequence 128 is superseded by an append-only record binding this decision text, the admission rule with its bounds, and the halted event identity (sequence + line SHA). The halted attempt's response is re-classified under this rule on resume (its 0.99 mass is admitted with deficit recorded) and settled as observed; settled attempts are never resent.\n- **Authorized scope exception**: the admission tolerance lives in the response-validation layer; a NARROW change there is hereby authorized \u2014 exactly this probability-mass rule and its recording. Pin equality, provider refusal, single-attempt semantics, and numeric fidelity remain frozen.\n\nRationale: the evaluation measures decision quality AND calibration separately; rejecting rounded-score distributions conflates formatting with correctness and censors a real quality signal. The typed-decision contract sloppiness stays visible as a counted violation."

const decision6RuleDescription = "decision-6-probability-mass-admission-v1: a Choice probability distribution is ADMITTED iff every score is finite and within [0,1] and the exact sum S satisfies |S - 1| <= 0.02 (adapter.ProbabilityMassAdmissionTolerance). Scores are preserved byte-verbatim and NEVER renormalized. The exact sum S and deficit (S - 1) are recorded on the outcome row; a deficit marker is set whenever |S - 1| > 0.001 (adapter.ProbabilityTolerance). Outside the class (|S - 1| > 0.02, missing/invalid fields, non-finite values, any structural failure) the existing not-admitted -> billing-invalid -> halt-both-arms path is unchanged. Deficit admissions continue and never halt."

// Decision6Digest is the SHA-256 of the verbatim Decision-6 text.
func Decision6Digest() string { return hashBytes([]byte(decision6Text)) }

// Decision6RuleDescription is the admission rule with its bounds, as bound
// into supersession records and the report basis.
func Decision6RuleDescription() string { return decision6RuleDescription }
