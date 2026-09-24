package pilot

// Independent adversarial probes for W1-R2 (raw response preservation).
// Reviewer: Jev Evaluation KHA-579 independent review of 9123bce.
// The preserved reproduction only exercised the screenRawJSON helper; these
// probes verify end-to-end THROUGH THE RUNNER: evidence files and report rows
// (replayed outcomes) must preserve valid nonempty bodies (screened/redacted),
// and malformed/trailing/ambiguous bodies must go hash-only.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
)

const advSecret = "adv-secret-key"

func advRunOnce(t *testing.T, rawBody string) (respFile, obsFile []byte, row Outcome, journal string) {
	t.Helper()
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1}}, 20, 10, 10)
	defer done()
	r.Config.ScreeningSecrets = []string{advSecret}
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		o := zeroObservation("jev")
		o.RawResponse = []byte(rawBody)
		return o, nil
	}
	outcomes, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes=%d", len(outcomes))
	}
	dir := r.Config.EvidenceDir
	respFile, err = os.ReadFile(filepath.Join(dir, "0001-x-jev.response"))
	if err != nil {
		t.Fatal(err)
	}
	obsFile, err = os.ReadFile(filepath.Join(dir, "0001-x-jev.observation.json"))
	if err != nil {
		t.Fatal(err)
	}
	journalBytes, err := os.ReadFile(r.Journal.path)
	if err != nil {
		t.Fatal(err)
	}
	// Report rows: replay from journal + evidence.
	replayed, err := ReplayOutcomes(r.Inventory, r.Journal.State(), dir)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(replayed) != 1 {
		t.Fatalf("replayed rows=%d", len(replayed))
	}
	return respFile, obsFile, replayed[0], string(journalBytes)
}

func decodeObservation(t *testing.T, raw []byte) ObservationEvidence {
	t.Helper()
	var ev ObservationEvidence
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatalf("observation file not valid JSON: %v", err)
	}
	return ev
}

// A valid nonempty body with secrets configured must be preserved
// (screened/redacted) in evidence files and report rows, with number lexemes
// unchanged, including the unicode-escaped secret form.
func TestAdvW1R2a_RunnerPreservesValidNonemptyBody(t *testing.T) {
	body := `{"id":"resp-1","usage":{"input_tokens":120,"output_tokens":205,"cost":0.000001},"diag":"reflected ` + advSecret + `","esc":"\u0061dv-secret-key","n":null}`
	resp, obsRaw, row, journal := advRunOnce(t, body)

	var parsed any
	if err := json.Unmarshal(resp, &parsed); err != nil {
		t.Fatalf("preserved response is not parseable JSON: %v\n%s", err, resp)
	}
	if strings.Contains(string(resp), advSecret) {
		t.Fatalf("secret leaked into preserved response: %s", resp)
	}
	if !bytes.Contains(resp, []byte(redactedCredential)) {
		t.Fatalf("expected redaction marker in preserved response: %s", resp)
	}
	for _, lex := range []string{"120", "205", "0.000001", "null"} {
		if !bytes.Contains(resp, []byte(lex)) {
			t.Fatalf("lexeme %q not preserved in response: %s", lex, resp)
		}
	}
	if bytes.Contains(resp, []byte(`"withheld":true`)) {
		t.Fatalf("valid body went hash-only: %s", resp)
	}
	ev := decodeObservation(t, obsRaw)
	if ev.RawWithheld {
		t.Fatalf("observation row reports rawWithheld: %+v", ev)
	}
	if !ev.EvidenceSanitized {
		t.Fatalf("expected evidenceSanitized: %+v", ev)
	}
	if row.Observation == nil || row.Observation.RawWithheld {
		t.Fatalf("report row withheld: %+v", row.Observation)
	}
	if strings.Contains(journal, advSecret) {
		t.Fatalf("secret leaked into journal: %s", journal)
	}
	t.Logf("preserved response: %s", resp)
}

// Malformed, trailing and ambiguous bodies must still go hash-only; a
// whitespace-only suffix is NOT trailing content and must be preserved;
// empty body and null-only body are preserved; secret-in-key is hash-only.
func TestAdvW1R2b_RunnerWithholdsOnlyUnsafeBodies(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		withhold bool
	}{
		{"malformed", `{"broken":`, true},
		{"two-top-level-values", `{"a":1}{"b":2}`, true},
		{"trailing-garbage", `{"a":1} xyz`, true},
		{"whitespace-only-suffix", "{\"a\":1}   \n", false},
		{"unicode-escaped-secret", `{"d":"\u0061dv-secret-key"}`, false},
		{"empty-body", ``, false},
		{"null-only-body", `null`, false},
		{"secret-in-key", `{"` + advSecret + `":"v"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, obsRaw, row, _ := advRunOnce(t, tc.body)
			ev := decodeObservation(t, obsRaw)
			if tc.withhold {
				if !bytes.Contains(resp, []byte(`"withheld":true`)) {
					t.Fatalf("unsafe body not hash-only: %s", resp)
				}
				if !ev.RawWithheld || row.Observation == nil || !row.Observation.RawWithheld {
					t.Fatalf("withheld marker missing from rows: ev=%+v row=%+v", ev, row.Observation)
				}
				if !strings.Contains(string(resp), "originalRawSha256") {
					t.Fatalf("hash-only marker missing original digest: %s", resp)
				}
			} else {
				if bytes.Contains(resp, []byte(`"withheld":true`)) {
					t.Fatalf("safe body wrongly withheld: %s", resp)
				}
				if ev.RawWithheld {
					t.Fatalf("safe body row reports withheld: %+v", ev)
				}
				if tc.body == `null` && string(resp) != `null` {
					t.Fatalf("null-only body not byte-identical: %q", resp)
				}
			}
			if strings.Contains(string(resp), advSecret) {
				t.Fatalf("secret leaked: %s", resp)
			}
		})
	}
}
