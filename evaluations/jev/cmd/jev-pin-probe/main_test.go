package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Clarit-AI/Plexium/evaluations/jev/adapter"
	"github.com/Clarit-AI/Plexium/evaluations/jev/probe"
)

func TestDryRunDoesNotRequireCredentialAndPrintsPlan(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	var out bytes.Buffer
	err := runCLI([]string{
		"--dry-run",
		"--inventory", filepath.Join("..", "..", "pilot", "request-inventory.json"),
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, `"version": "jev-pin-probe-v1"`) || !strings.Contains(text, `"credentialEnv": "OPENROUTER_API_KEY"`) {
		t.Fatalf("unexpected dry plan: %s", text)
	}
}

func TestDryRunBindsApprovedRequestSubset(t *testing.T) {
	var credentialReads int32
	lookup := func(string) (string, bool) {
		atomic.AddInt32(&credentialReads, 1)
		return "must-not-be-read", true
	}
	var out bytes.Buffer
	err := runCLIWithConfigAndCredential([]string{
		"--dry-run", "--request-ordinals=2,3,4",
		"--inventory", filepath.Join("..", "..", "pilot", "request-inventory.json"),
	}, &out, probe.DefaultRunConfig, lookup)
	if err != nil {
		t.Fatal(err)
	}
	var plan probe.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("decode dry plan: %v output=%s", err, out.String())
	}
	if credentialReads != 0 || fmt.Sprint(plan.SelectedOrdinals) != "[2 3 4]" || len(plan.SelectedRequests) != 3 {
		t.Fatalf("credentialReads=%d selection=%v bindings=%+v", credentialReads, plan.SelectedOrdinals, plan.SelectedRequests)
	}
	for index, ordinal := range []int{2, 3, 4} {
		if plan.SelectedRequests[index].Ordinal != ordinal || plan.SelectedRequests[index].RequestSHA256 == "" {
			t.Fatalf("dry plan selection row %d is unbound: %+v", ordinal, plan.SelectedRequests[index])
		}
	}
}

func TestDeriveAssessmentReadsNoCredentialAndPreservesSource(t *testing.T) {
	inventory := filepath.Join("..", "..", "pilot", "request-inventory.json")
	plan, err := probe.BuildPlan(inventory, probe.Endpoints{Jev: probe.JevEndpoint, Nano: probe.NanoEndpoint})
	if err != nil {
		t.Fatal(err)
	}
	report := probe.Report{
		Version: probe.PlanVersion, InventorySHA256: plan.InventorySHA256,
		SelectedOrdinals: append([]int(nil), plan.SelectedOrdinals...),
		SelectedRequests: append([]probe.RequestBinding(nil), plan.SelectedRequests...),
	}
	for _, binding := range plan.SelectedRequests {
		report.Attempts = append(report.Attempts, probe.Attempt{
			Ordinal: binding.Ordinal, RequestID: binding.ID, Arm: binding.Arm, Kind: binding.Kind,
			FixtureID: binding.FixtureID, RequestedAlias: binding.RequestedAlias,
			ExpectedPin: binding.CandidatePin, ExpectedProvider: binding.CandidateProvider,
			RequestSHA256: binding.RequestSHA256, ReservationRef: fmt.Sprintf("res-test-%d", binding.Ordinal),
			RawResponseSHA256: fmt.Sprintf("%064x", binding.Ordinal), EvidenceSHA256: fmt.Sprintf("%064x", binding.Ordinal+4),
			State: "settled", RequestSent: true,
			ResponseReceived: true, Status: 200, ProviderRequestID: "provider-request",
			ResolvedModel: binding.CandidatePin, ResponseProvider: binding.CandidateProvider,
			Billing: probe.BillingEvidence{Cost: probe.DecimalEvidence{Present: true, Valid: true, Raw: "0.000001"}},
		})
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "source.json")
	data, err := json.MarshalIndent(&report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(data)
	output := filepath.Join(dir, "derived.json")

	var credentialReads, factoryCalls int32
	lookup := func(string) (string, bool) {
		atomic.AddInt32(&credentialReads, 1)
		return "must-not-be-read", true
	}
	factory := func(inventoryPath, state, key string) probe.RunConfig {
		atomic.AddInt32(&factoryCalls, 1)
		return probe.DefaultRunConfig(inventoryPath, state, key)
	}
	var out bytes.Buffer
	err = runCLIWithConfigAndCredential([]string{
		"--derive-assessment", "--inventory", inventory,
		"--source-reports", source, "--assessment-out", output,
	}, &out, factory, lookup)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != before || credentialReads != 0 || factoryCalls != 0 {
		t.Fatalf("source changed or forbidden callback ran: credentialReads=%d factoryCalls=%d", credentialReads, factoryCalls)
	}
	var assessment probe.DerivedAssessment
	if err := json.Unmarshal(out.Bytes(), &assessment); err != nil {
		t.Fatalf("decode assessment: %v output=%s", err, out.String())
	}
	if assessment.Gates[0].Status != "EVIDENCE_COLLECTED" || assessment.Gates[1].Status != "EVIDENCE_COLLECTED" {
		t.Fatalf("derived gate coverage missing: %+v", assessment.Gates)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("derived output missing: %v", err)
	}
}

func TestExecuteRequiresCredentialBeforeAnyRun(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	var out bytes.Buffer
	err := runCLI([]string{
		"--execute", "--state-dir", t.TempDir(),
		"--inventory", filepath.Join("..", "..", "pilot", "request-inventory.json"),
	}, &out)
	if err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
}

func TestExecuteRequestSubsetSendsOnlyBoundPlanEntries(t *testing.T) {
	inventory := filepath.Join("..", "..", "pilot", "request-inventory.json")
	plan, err := probe.BuildPlan(inventory, probe.Endpoints{Jev: probe.JevEndpoint, Nano: probe.NanoEndpoint})
	if err != nil {
		t.Fatal(err)
	}
	expected := make(map[int]string, len(plan.Requests))
	for _, request := range plan.Requests {
		expected[request.Ordinal] = request.RequestSHA256
	}

	server, calls, hashes := cliProbeServer(t)
	defer server.Close()
	factory := func(inventoryPath, state, key string) probe.RunConfig {
		cfg := probe.DefaultRunConfig(inventoryPath, state, key)
		cfg.Endpoints = probe.Endpoints{Jev: server.URL + "/api/alpha/decisions", Nano: server.URL + "/api/v1/chat/completions"}
		cfg.HTTPClient = server.Client()
		cfg.Timeout = time.Second
		return cfg
	}

	for _, test := range []struct {
		name         string
		selectionArg []string
		ordinals     []int
	}{
		{name: "approved-subset", selectionArg: []string{"--request-ordinals=2,3,4"}, ordinals: []int{2, 3, 4}},
		{name: "no-flag-full-plan", ordinals: []int{1, 2, 3, 4}},
	} {
		t.Run(test.name, func(t *testing.T) {
			atomic.StoreInt32(calls, 0)
			hashes.reset()
			t.Setenv(probe.CredentialEnv, "mock-only-credential")
			args := []string{"--execute", "--state-dir", t.TempDir(), "--inventory", inventory}
			args = append(args, test.selectionArg...)
			var out bytes.Buffer
			if err := runCLIWithConfig(args, &out, factory); err != nil {
				t.Fatalf("execute: %v output=%s", err, out.String())
			}
			if int(atomic.LoadInt32(calls)) != len(test.ordinals) {
				t.Fatalf("calls=%d want=%d", atomic.LoadInt32(calls), len(test.ordinals))
			}
			seen := hashes.values()
			if len(seen) != len(test.ordinals) {
				t.Fatalf("seen hashes=%v", seen)
			}
			for index, ordinal := range test.ordinals {
				if seen[index] != expected[ordinal] {
					t.Fatalf("call %d hash=%s want ordinal %d hash=%s", index, seen[index], ordinal, expected[ordinal])
				}
			}
			if test.name == "approved-subset" {
				for _, seenHash := range seen {
					if seenHash == expected[1] {
						t.Fatalf("ordinal 1 was dialed under approved subset: %v", seen)
					}
				}
			}
			var report probe.Report
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("decode report: %v output=%s", err, out.String())
			}
			if fmt.Sprint(report.SelectedOrdinals) != fmt.Sprint(test.ordinals) || len(report.SelectedRequests) != len(test.ordinals) || len(report.Attempts) != len(test.ordinals) {
				t.Fatalf("report selection mismatch: %+v", report)
			}
			for index, ordinal := range test.ordinals {
				if report.SelectedRequests[index].Ordinal != ordinal || report.SelectedRequests[index].RequestSHA256 != expected[ordinal] || report.Attempts[index].Ordinal != ordinal {
					t.Fatalf("report row %d not bound to ordinal %d: selected=%+v attempt=%+v", index, ordinal, report.SelectedRequests[index], report.Attempts[index])
				}
			}
		})
	}
}

func TestInvalidRequestSelectionsRefuseBeforeCredentialReadOrDial(t *testing.T) {
	inventory := filepath.Join("..", "..", "pilot", "request-inventory.json")
	for _, selection := range []string{"0", "5", "1,1", "2-4", "all", "", "2,", "x", "2,3,4,5"} {
		t.Run("selection-"+selection, func(t *testing.T) {
			var credentialReads, factoryCalls, dialCalls int32
			lookup := func(string) (string, bool) {
				atomic.AddInt32(&credentialReads, 1)
				return "must-not-be-read", true
			}
			factory := func(inventoryPath, state, key string) probe.RunConfig {
				atomic.AddInt32(&factoryCalls, 1)
				cfg := probe.DefaultRunConfig(inventoryPath, state, key)
				cfg.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					atomic.AddInt32(&dialCalls, 1)
					return nil, fmt.Errorf("unexpected dial")
				})}
				return cfg
			}
			var out bytes.Buffer
			err := runCLIWithConfigAndCredential([]string{
				"--execute", "--state-dir", t.TempDir(), "--inventory", inventory,
				"--request-ordinals=" + selection,
			}, &out, factory, lookup)
			if err == nil || credentialReads != 0 || factoryCalls != 0 || dialCalls != 0 || out.Len() != 0 {
				t.Fatalf("selection=%q err=%v credentialReads=%d factoryCalls=%d dialCalls=%d output=%s", selection, err, credentialReads, factoryCalls, dialCalls, out.String())
			}
		})
	}
}

func TestUnexpectedArgumentsAndRepeatedFlagsRefuseBeforeCredentialReadOrDial(t *testing.T) {
	inventory := filepath.Join("..", "..", "pilot", "request-inventory.json")
	for _, test := range []struct {
		name string
		args []string
	}{
		{
			name: "stray-before-selector",
			args: []string{"--execute", "--state-dir", "unused", "--inventory", inventory, "stray", "--request-ordinals=2,3,4"},
		},
		{
			name: "double-dash-before-selector",
			args: []string{"--execute", "--state-dir", "unused", "--inventory", inventory, "--", "--request-ordinals=2,3,4"},
		},
		{
			name: "leftover-after-valid-selector",
			args: []string{"--execute", "--state-dir", "unused", "--inventory", inventory, "--request-ordinals=2,3,4", "stray"},
		},
		{
			name: "repeated-selector-invalid-then-valid",
			args: []string{"--execute", "--state-dir", "unused", "--inventory", inventory, "--request-ordinals=3,3", "--request-ordinals=2,3,4"},
		},
		{
			name: "repeated-selector-conflicting-valid",
			args: []string{"--execute", "--state-dir", "unused", "--inventory", inventory, "--request-ordinals", "2,3", "--request-ordinals=2,3,4"},
		},
		{
			name: "repeated-state-dir",
			args: []string{"--execute", "--state-dir", "first", "--state-dir", "second", "--inventory", inventory},
		},
		{
			name: "repeated-execute",
			args: []string{"--execute", "--execute", "--state-dir", "unused", "--inventory", inventory},
		},
		{
			name: "state-dir-swallows-selector",
			args: []string{"--inventory", inventory, "--execute", "--state-dir", "--request-ordinals=2,3,4"},
		},
		{
			name: "state-dir-flag-looking-value",
			args: []string{"--execute", "--state-dir", "-x", "--inventory", inventory},
		},
		{
			name: "inventory-swallows-execute",
			args: []string{"--inventory", "--execute", "--state-dir", "unused"},
		},
		{
			name: "selector-swallows-dry-run",
			args: []string{"--request-ordinals", "--dry-run"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var credentialReads, factoryCalls, dialCalls int32
			lookup := func(string) (string, bool) {
				atomic.AddInt32(&credentialReads, 1)
				return "must-not-be-read", true
			}
			factory := func(inventoryPath, state, key string) probe.RunConfig {
				atomic.AddInt32(&factoryCalls, 1)
				cfg := probe.DefaultRunConfig(inventoryPath, state, key)
				cfg.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					atomic.AddInt32(&dialCalls, 1)
					return nil, fmt.Errorf("unexpected dial")
				})}
				return cfg
			}
			var out bytes.Buffer
			err := runCLIWithConfigAndCredential(test.args, &out, factory, lookup)
			if err == nil || credentialReads != 0 || factoryCalls != 0 || dialCalls != 0 || out.Len() != 0 {
				t.Fatalf("err=%v credentialReads=%d factoryCalls=%d dialCalls=%d output=%s", err, credentialReads, factoryCalls, dialCalls, out.String())
			}
		})
	}
}

func TestLeadingHyphenPathWithDotSlashIsAccepted(t *testing.T) {
	inventory := filepath.Join("..", "..", "pilot", "request-inventory.json")
	var credentialReads int32
	lookup := func(string) (string, bool) {
		atomic.AddInt32(&credentialReads, 1)
		return "must-not-be-read", true
	}
	var out bytes.Buffer
	err := runCLIWithConfigAndCredential([]string{
		"--dry-run", "--state-dir", "./-weirdname", "--inventory", inventory,
	}, &out, probe.DefaultRunConfig, lookup)
	if err != nil {
		t.Fatalf("dot-slash leading-hyphen path rejected: %v", err)
	}
	if credentialReads != 0 {
		t.Fatalf("credential reads=%d, want 0", credentialReads)
	}
	if out.Len() == 0 {
		t.Fatal("dry-run emitted no plan")
	}
}

func TestExecuteRedactsReflectedCredentialFromCLIAndFiles(t *testing.T) {
	secret := "fake-cli-key-reflected-579"
	t.Setenv(probe.CredentialEnv, secret)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":20,"output_tokens":2,"cost":0.000001,"diagnostic":"\u0066ake-cli-key-reflected-579"}}`, probe.JevCandidatePin, probe.JevCandidateProvider)
	}))
	defer server.Close()
	stateDir := t.TempDir()
	factory := func(inventory, state, key string) probe.RunConfig {
		cfg := probe.DefaultRunConfig(inventory, state, key)
		cfg.Endpoints = probe.Endpoints{Jev: server.URL + "/api/alpha/decisions", Nano: server.URL + "/api/v1/chat/completions"}
		cfg.HTTPClient = server.Client()
		cfg.Timeout = time.Second
		return cfg
	}
	var out bytes.Buffer
	err := runCLIWithConfig([]string{
		"--execute", "--state-dir", stateDir,
		"--inventory", filepath.Join("..", "..", "pilot", "request-inventory.json"),
	}, &out, factory)
	if err == nil {
		t.Fatal("expected schema halt")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked to CLI output: stdout=%s stderr=%v", out.String(), err)
	}
	assertDecodedNoCredential(t, out.Bytes(), secret)
	err = filepath.WalkDir(stateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.HasSuffix(path, ".json") {
			if checkErr := decodedHasCredential(data, secret); checkErr != nil {
				return fmt.Errorf("credential leaked in %s: %w", path, checkErr)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExecuteInvalidStringCostCannotLeakThroughCLIOrFiles(t *testing.T) {
	secret := "test-key-never-printed"
	t.Setenv(probe.CredentialEnv, secret)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":"supported"}},"usage":{"input_tokens":20,"output_tokens":2,"cost":"test-key-never-printed"}}`, probe.JevCandidatePin, probe.JevCandidateProvider)
	}))
	defer server.Close()
	stateDir := t.TempDir()
	factory := func(inventory, state, key string) probe.RunConfig {
		cfg := probe.DefaultRunConfig(inventory, state, key)
		cfg.Endpoints = probe.Endpoints{Jev: server.URL + "/api/alpha/decisions", Nano: server.URL + "/api/v1/chat/completions"}
		cfg.HTTPClient = server.Client()
		cfg.Timeout = time.Second
		return cfg
	}
	var out bytes.Buffer
	err := runCLIWithConfig([]string{
		"--execute", "--state-dir", stateDir,
		"--inventory", filepath.Join("..", "..", "pilot", "request-inventory.json"),
	}, &out, factory)
	if err == nil {
		t.Fatal("expected invalid-billing halt")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked in CLI error: %v", err)
	}
	assertDecodedNoCredential(t, out.Bytes(), secret)
	var report probe.Report
	if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil {
		t.Fatalf("decode CLI report: %v", decodeErr)
	}
	if len(report.Attempts) != 1 || report.Attempts[0].Billing.Cost.Valid || report.Attempts[0].Billing.Cost.Raw != `"[REDACTED_CREDENTIAL]"` {
		t.Fatalf("CLI report lost invalid/untrusted billing distinction: %+v", report.Attempts)
	}
	err = filepath.WalkDir(stateDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".json") {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if checkErr := decodedHasCredential(data, secret); checkErr != nil {
			return fmt.Errorf("credential leaked in %s: %w", path, checkErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertDecodedNoCredential(t *testing.T, data []byte, credential string) {
	t.Helper()
	if err := decodedHasCredential(data, credential); err != nil {
		t.Fatal(err)
	}
}

func decodedHasCredential(data []byte, credential string) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	var walk func(any) error
	walk = func(current any) error {
		switch typed := current.(type) {
		case string:
			if strings.Contains(typed, credential) {
				return fmt.Errorf("decoded credential remains in %q", typed)
			}
		case []any:
			for _, item := range typed {
				if err := walk(item); err != nil {
					return err
				}
			}
		case map[string]any:
			for key, item := range typed {
				if strings.Contains(key, credential) {
					return fmt.Errorf("decoded credential remains in key %q", key)
				}
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}

type requestHashes struct {
	mu     sync.Mutex
	hashes []string
}

func (h *requestHashes) add(body []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sum := sha256.Sum256(body)
	h.hashes = append(h.hashes, fmt.Sprintf("%x", sum[:]))
}

func (h *requestHashes) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hashes = nil
}

func (h *requestHashes) values() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.hashes...)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func cliProbeServer(t *testing.T) (*httptest.Server, *int32, *requestHashes) {
	t.Helper()
	var calls int32
	hashes := &requestHashes{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		hashes.add(body)
		switch request.URL.Path {
		case "/api/alpha/decisions":
			var payload adapter.DecisionRequest
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode Decisions request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			labels := make([]string, 0, len(payload.Questions[adapter.DecisionVerdictID].Criteria))
			for label := range payload.Questions[adapter.DecisionVerdictID].Criteria {
				labels = append(labels, label)
			}
			sort.Strings(labels)
			fmt.Fprintf(w, `{"id":"d","model":%q,"provider":%q,"answers":{"verdict":{"type":"choice","choice":%q}},"usage":{"input_tokens":20,"output_tokens":2,"cost":0.000001}}`, probe.JevCandidatePin, probe.JevCandidateProvider, labels[0])
		case "/api/v1/chat/completions":
			var payload adapter.ChatRequest
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode Nano request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			properties := payload.ResponseFormat.JSONSchema.Schema["properties"].(map[string]any)
			labelSchema := properties["label"].(map[string]any)
			enum := labelSchema["enum"].([]any)
			label := enum[0].(string)
			content, _ := json.Marshal(map[string]string{"label": label})
			fmt.Fprintf(w, `{"id":"c","object":"chat.completion","model":%q,"provider":%q,"choices":[{"index":0,"message":{"role":"assistant","content":%q,"annotations":[]},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22,"cost":0.000001,"completion_tokens_details":{"accepted_prediction_tokens":0,"rejected_prediction_tokens":0}}}`, probe.NanoCandidatePin, probe.NanoProvider, string(content))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return server, &calls, hashes
}
