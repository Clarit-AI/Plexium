package pilot

// Boundary-matrix probes against the NEW sanitizeTextPreservingNumbers API
// (only present in 9123bce).

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
)

const advFakeKeyB = "20"

// Direction B2: sanitizeTextPreservingNumbers boundary matrix.
// Cost lexemes embedded in error/halt text must stay untouched; genuine
// credentials — numeric-shaped or numeric-adjacent — must still be redacted.
func TestAdvW1R3c_SanitizeTextPreservingNumbersBoundary(t *testing.T) {
	cases := []struct {
		text string
		want string // desired output
	}{
		// cost lexemes / numeric diagnostics in text: untouched
		{"settled cost 3.20 ok", "settled cost 3.20 ok"},
		{"rate is 20.5", "rate is 20.5"},
		{"exponent 2e20", "exponent 2e20"},
		{"cost 0.0000020", "cost 0.0000020"},
		{"counters 120/205", "counters 120/205"},
		{"total 0.000001", "total 0.000001"},
		{"ref 992088", "ref 992088"},
		// genuine credentials must be redacted
		{"code 20 rejected", "code " + redactedCredential + " rejected"},
		{"key sk-20-live", "key sk-" + redactedCredential + "-live"},
		{"key v20beta invalid", "key v" + redactedCredential + "beta invalid"},
		{"model gpt-20turbo", "model gpt-" + redactedCredential + "turbo"},
		{"token=20;", "token=" + redactedCredential + ";"},
	}
	var corrupted, leaked []string
	for _, tc := range cases {
		got := sanitizeTextPreservingNumbers(tc.text, []string{advFakeKeyB})
		if got != tc.want {
			if strings.Contains(got, redactedCredential) && !strings.Contains(tc.want, redactedCredential) {
				corrupted = append(corrupted, fmt.Sprintf("%q -> %q (numeric corrupted)", tc.text, got))
			} else if !strings.Contains(got, redactedCredential) && strings.Contains(tc.want, redactedCredential) {
				leaked = append(leaked, fmt.Sprintf("%q -> %q (credential leaked)", tc.text, got))
			} else {
				corrupted = append(corrupted, fmt.Sprintf("%q -> %q (want %q)", tc.text, got, tc.want))
			}
		}
	}
	if len(corrupted) > 0 {
		t.Errorf("W1-R3 NUMERIC CORRUPTION in error/halt text:\n  %s", strings.Join(corrupted, "\n  "))
	}
	if len(leaked) > 0 {
		t.Errorf("W1-R3 CREDENTIAL LEAK in error/halt text:\n  %s", strings.Join(leaked, "\n  "))
	}
}

// Direction B3: the same boundary through the Runner into journal rows and
// the halt event: error text quoting a cost decimal must keep it, and a
// numeric-adjacent credential must not survive.
func TestAdvW1R3d_ErrorHaltTextThroughRunner(t *testing.T) {
	r, done := newTestRunner(t, []Slot{{Ordinal: 1, FixtureID: "x", Arm: ArmJev, Repetition: 1}}, 20, 10, 10)
	defer done()
	r.Config.ScreeningSecrets = []string{advFakeKeyB}
	r.Attempts[ArmJev] = func(context.Context, []byte) (adapter.AttemptObservation, error) {
		return zeroObservation("jev"), fmt.Errorf("settlement shows cost 3.20 for key v20beta")
	}
	_, _ = r.Run(context.Background())
	journalBytes, err := os.ReadFile(r.Journal.path)
	if err != nil {
		t.Fatal(err)
	}
	journal := string(journalBytes)
	if !strings.Contains(journal, "3.20") {
		t.Errorf("W1-R3 NUMERIC CORRUPTION through Runner: decimal 3.20 destroyed in journal/halt text:\n%s", journal)
	}
	if strings.Contains(journal, "v20beta") {
		t.Errorf("W1-R3 CREDENTIAL LEAK through Runner: numeric-adjacent credential survived in journal/halt text:\n%s", journal)
	}
}
