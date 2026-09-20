// Command jev-dryrun computes an offline cost projection for a Jev
// evaluation run. It makes ZERO network calls. The projection uses
// explicitly configured (unverified) rates, token limits, and retry
// assumptions to emit a deterministic plan with:
//   - Per-task and per-attempt reservation estimates
//   - Retry ceiling and discovery cost
//   - Maximum possible cost under the plan
//   - Missing approvals (e.g., authorized cap = $0 by default)
//
// This is a PLANNING TOOL ONLY. It does not reserve, settle, or dial out.
// All monetary amounts are in micro-units (1/1,000,000 of base unit).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	jevledger "github.com/Clarit-AI/Plexium/evaluations/jev/ledger"
)

func main() {
	var (
		fixturesPath   = flag.String("fixtures", "evaluations/jev/fixtures.jsonl", "path to fixture JSONL")
		manifestPath   = flag.String("manifest", "evaluations/jev/fixtures.manifest.json", "path to manifest")
		model          = flag.String("model", "", "model identifier (required)")
		rateIn         = flag.Float64("rate-in", 0, "input rate in base units per 1M tokens (e.g., 0.042)")
		rateOut        = flag.Float64("rate-out", 0, "output rate in base units per 1M tokens (e.g., 0.042)")
		maxIn          = flag.Int("max-input-tokens", 4096, "conservative max input tokens per attempt")
		maxOut         = flag.Int("max-output-tokens", 2048, "conservative max output tokens per attempt")
		maxRetries     = flag.Int("max-retries", 1, "max retries per attempt")
		retryMult      = flag.Float64("retry-multiplier", 1.0, "cost multiplier per retry (1.0 = full cost)")
		discoveryMult  = flag.Float64("discovery-multiplier", 0.5, "discovery baseline cost multiplier")
		discoveryCost  = flag.Float64("discovery-cost", 0, "estimated discovery baseline cost in base units")
		cap            = flag.Float64("cap", 0, "authorized spend cap in base units (default 0)")
		ratesVersion   = flag.String("rates-version", "unverified-"+time.Now().UTC().Format("2006-01-02"), "rates source identifier")
		protocolVers   = flag.String("protocol-version", "0.3.0", "protocol version")
		fixtureSHA     = flag.String("fixture-sha", "", "fixture file SHA256 (computed if empty)")
		tokenBoundsSHA = flag.String("token-bounds-sha", "", "token bounds config SHA256 (computed if empty)")
		outPath        = flag.String("out", "-", "output JSON path; - for stdout")
		jsonOutput     = flag.Bool("json", true, "output JSON (vs human-readable)")
		verbose        = flag.Bool("v", false, "verbose output")
		repetitions    = flag.Int("repetitions", 3, "number of repetitions per fixture (protocol default: 3 for held-out scoring)")
	)
	flag.Parse()

	if *model == "" {
		die("model is required (use -model)")
	}
	if *rateIn <= 0 || *rateOut <= 0 {
		die("rate-in and rate-out must be > 0")
	}
	if *maxIn <= 0 || *maxOut <= 0 {
		die("max-input-tokens and max-output-tokens must be > 0")
	}
	if *maxRetries < 0 {
		die("max-retries must be >= 0")
	}
	if *repetitions < 1 {
		die("repetitions must be >= 1")
	}
	if *retryMult < 0 || *discoveryMult < 0 || *discoveryCost < 0 || *cap < 0 {
		die("multipliers, costs, and cap must be >= 0")
	}

	// Load manifest to get fixture count, task split, source groups.
	manifest, err := loadManifest(*manifestPath)
	if err != nil {
		die("load manifest: %v", err)
	}

	// Load fixtures to get per-task per-group breakdown.
	fixtures, err := loadFixtures(*fixturesPath)
	if err != nil {
		die("load fixtures: %v", err)
	}

	// Compute fixture SHA if not provided.
	if *fixtureSHA == "" {
		*fixtureSHA = manifest.SHA256FixtureFile
	}
	if *fixtureSHA == "" {
		die("fixture SHA required (provide -fixture-sha or ensure manifest has it)")
	}
	if *tokenBoundsSHA == "" {
		*tokenBoundsSHA = hashString(fmt.Sprintf("%d:%d:%d:%f:%f:%f", *maxIn, *maxOut, *maxRetries, *retryMult, *discoveryMult, *discoveryCost))
	}

	// Convert rates to micro-units.
	rateInMicro := MicroUnitFromBase(*rateIn)
	rateOutMicro := MicroUnitFromBase(*rateOut)
	capMicro := MicroUnitFromBase(*cap)
	discoveryCostMicro := MicroUnitFromBase(*discoveryCost)

	// Build task plan: count attempts per task per source group.
	// Each fixture = one attempt per repetition.
	taskGroups := analyzeFixtures(fixtures)

	// Compute per-task estimates.
	plan := DryRunPlan{
		RunID:               "dryrun-" + time.Now().UTC().Format("20060102-150405"),
		Model:               *model,
		ProtocolVersion:     *protocolVers,
		FixturesPath:        *fixturesPath,
		ManifestPath:        *manifestPath,
		FixtureSHA:          *fixtureSHA,
		RatesVersion:        *ratesVersion,
		TokenBoundsSHA:      *tokenBoundsSHA,
		GeneratedAt:         time.Now().UTC(),
		RateIn:              *rateIn,
		RateOut:             *rateOut,
		MaxInputTokens:      *maxIn,
		MaxOutputTokens:     *maxOut,
		MaxRetries:          *maxRetries,
		RetryMultiplier:     *retryMult,
		DiscoveryMultiplier: *discoveryMult,
		DiscoveryCost:       *discoveryCost,
		AuthorizedCap:       *cap,
		Repetitions:         *repetitions,
		TokenBounds:         TokenBounds{MaxInputTokens: *maxIn, MaxOutputTokens: *maxOut},
		RetryPolicy:         RetryPolicy{MaxRetries: *maxRetries, RetryMultiplier: *retryMult, DiscoveryMultiplier: *discoveryMult},
	}

	// Per-task estimates.
	for task, groups := range taskGroups {
		var taskTotal jevledger.MicroUnit
		taskEst := TaskEstimate{
			Task:         task,
			FixtureCount: len(groups),
			SourceGroups: make([]SourceGroupEstimate, 0, len(groups)),
		}
		for _, sg := range groups {
			// Each fixture in this group for this task = 1 attempt per repetition.
			attempts := len(sg.FixtureIDs) * *repetitions
			basePerAttempt := estimateCostMicro(rateInMicro, rateOutMicro, int64(*maxIn), int64(*maxOut))
			retryBudget := jevledger.MicroUnit(float64(basePerAttempt) * *retryMult * float64(*maxRetries))
			discoveryBudget := jevledger.MicroUnit(float64(discoveryCostMicro) * *discoveryMult)
			totalPerGroup := jevledger.MicroUnit(attempts)*(basePerAttempt+retryBudget) + discoveryBudget

			taskTotal += totalPerGroup
			taskEst.SourceGroups = append(taskEst.SourceGroups, SourceGroupEstimate{
				SourceGroup:     sg.SourceGroup,
				FixtureCount:    len(sg.FixtureIDs),
				Attempts:        attempts,
				BasePerAttempt:  basePerAttempt,
				RetryBudget:     retryBudget,
				DiscoveryBudget: discoveryBudget,
				TotalReserved:   totalPerGroup,
			})
		}
		// Sort source groups for deterministic output.
		sort.Slice(taskEst.SourceGroups, func(i, j int) bool {
			return taskEst.SourceGroups[i].SourceGroup < taskEst.SourceGroups[j].SourceGroup
		})
		taskEst.TotalReserved = taskTotal
		plan.TaskEstimates = append(plan.TaskEstimates, taskEst)
	}

	// Sort tasks for deterministic output.
	sort.Slice(plan.TaskEstimates, func(i, j int) bool {
		return plan.TaskEstimates[i].Task < plan.TaskEstimates[j].Task
	})

	// Compute totals.
	for _, te := range plan.TaskEstimates {
		plan.TotalReserved += te.TotalReserved
		plan.TotalFixtures += te.FixtureCount
	}
	plan.TotalAvailable = capMicro - plan.TotalReserved
	plan.CapExceeded = plan.TotalReserved > capMicro

	// Missing approvals.
	if capMicro == 0 {
		plan.MissingApprovals = append(plan.MissingApprovals, "authorized cap is $0 (use -cap to set)")
	}
	if *rateIn == 0 || *rateOut == 0 {
		plan.MissingApprovals = append(plan.MissingApprovals, "rates are unverified quotes (not confirmed requestable)")
	}
	plan.MissingApprovals = append(plan.MissingApprovals, "no human label adjudication (all fixtures unreviewed)")
	plan.MissingApprovals = append(plan.MissingApprovals, "no live model comparison baseline")
	plan.MissingApprovals = append(plan.MissingApprovals, "corpus does not meet protocol sample-size targets (tuning <30/task, held-out negatives <150)")

	// Write output.
	if err := writeOutput(*outPath, plan, *jsonOutput, *verbose); err != nil {
		die("write output: %v", err)
	}

	// Exit code: 0 = plan valid, 1 = cap exceeded, 2 = other error.
	if plan.CapExceeded {
		fmt.Fprintf(os.Stderr, "DRY-RUN: PLAN EXCEEDS CAP (reserved %s > cap %s)\n", formatMicro(plan.TotalReserved), formatMicro(capMicro))
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "DRY-RUN: plan reserved %s, cap %s, available %s\n", formatMicro(plan.TotalReserved), formatMicro(capMicro), formatMicro(plan.TotalAvailable))
}

// DryRunPlan is the complete offline cost projection.
type DryRunPlan struct {
	RunID               string              `json:"runId"`
	Model               string              `json:"model"`
	ProtocolVersion     string              `json:"protocolVersion"`
	FixturesPath        string              `json:"fixturesPath"`
	ManifestPath        string              `json:"manifestPath"`
	FixtureSHA          string              `json:"fixtureSha"`
	RatesVersion        string              `json:"ratesVersion"`
	TokenBoundsSHA      string              `json:"tokenBoundsSha"`
	GeneratedAt         time.Time           `json:"generatedAt"`
	RateIn              float64             `json:"rateIn"`
	RateOut             float64             `json:"rateOut"`
	MaxInputTokens      int                 `json:"maxInputTokens"`
	MaxOutputTokens     int                 `json:"maxOutputTokens"`
	MaxRetries          int                 `json:"maxRetries"`
	RetryMultiplier     float64             `json:"retryMultiplier"`
	DiscoveryMultiplier float64             `json:"discoveryMultiplier"`
	DiscoveryCost       float64             `json:"discoveryCost"`
	AuthorizedCap       float64             `json:"authorizedCap"`
	Repetitions         int                 `json:"repetitions"`
	TokenBounds         TokenBounds         `json:"tokenBounds"`
	RetryPolicy         RetryPolicy         `json:"retryPolicy"`
	TaskEstimates       []TaskEstimate      `json:"taskEstimates"`
	TotalFixtures       int                 `json:"totalFixtures"`
	TotalReserved       jevledger.MicroUnit `json:"totalReservedMicro"`
	TotalAvailable      jevledger.MicroUnit `json:"totalAvailableMicro"`
	CapExceeded         bool                `json:"capExceeded"`
	MissingApprovals    []string            `json:"missingApprovals"`
}

// TaskEstimate is the per-task reservation estimate.
type TaskEstimate struct {
	Task          string                `json:"task"`
	FixtureCount  int                   `json:"fixtureCount"`
	SourceGroups  []SourceGroupEstimate `json:"sourceGroups"`
	TotalReserved jevledger.MicroUnit   `json:"totalReservedMicro"`
}

// SourceGroupEstimate is the per-source-group reservation estimate.
type SourceGroupEstimate struct {
	SourceGroup     string              `json:"sourceGroup"`
	FixtureCount    int                 `json:"fixtureCount"`
	Attempts        int                 `json:"attempts"`
	BasePerAttempt  jevledger.MicroUnit `json:"basePerAttemptMicro"`
	RetryBudget     jevledger.MicroUnit `json:"retryBudgetMicro"`
	DiscoveryBudget jevledger.MicroUnit `json:"discoveryBudgetMicro"`
	TotalReserved   jevledger.MicroUnit `json:"totalReservedMicro"`
}

// TokenBounds mirrors ledger.TokenBounds.
type TokenBounds struct {
	MaxInputTokens  int `json:"maxInputTokens"`
	MaxOutputTokens int `json:"maxOutputTokens"`
}

// RetryPolicy mirrors ledger.ReservationRetryPolicy.
type RetryPolicy struct {
	MaxRetries          int     `json:"maxRetries"`
	RetryMultiplier     float64 `json:"retryMultiplier"`
	DiscoveryMultiplier float64 `json:"discoveryMultiplier"`
}

// FixtureInfo is the minimal fixture data needed for planning.
type FixtureInfo struct {
	ID             string
	Task           string
	SourceGroup    string
	TemplateFamily string
}

// ManifestSummary is the minimal manifest data needed.
type ManifestSummary struct {
	SHA256FixtureFile string
	ProtocolVersion   string
	SplitCounts       SplitCounts
	TaskCounts        TaskCounts
}

type SplitCounts struct {
	Tuning  int `json:"tuning"`
	HeldOut int `json:"heldOut"`
	Total   int `json:"total"`
}

type TaskCounts struct {
	EntityType    int `json:"entityType"`
	CandidateType int `json:"candidateType"`
	Relationship  int `json:"relationship"`
	ClaimSupport  int `json:"claimSupport"`
	Total         int `json:"total"`
}

type SourceGroupFixtures struct {
	SourceGroup string
	FixtureIDs  []string
}

func analyzeFixtures(fixtures []FixtureInfo) map[string][]SourceGroupFixtures {
	// Map: task -> sourceGroup -> fixture IDs
	taskMap := make(map[string]map[string][]string)
	for _, f := range fixtures {
		if taskMap[f.Task] == nil {
			taskMap[f.Task] = make(map[string][]string)
		}
		taskMap[f.Task][f.SourceGroup] = append(taskMap[f.Task][f.SourceGroup], f.ID)
	}

	// Convert to slice form for deterministic ordering.
	result := make(map[string][]SourceGroupFixtures)
	for task, groups := range taskMap {
		var list []SourceGroupFixtures
		for sg, ids := range groups {
			sort.Strings(ids)
			list = append(list, SourceGroupFixtures{SourceGroup: sg, FixtureIDs: ids})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].SourceGroup < list[j].SourceGroup })
		result[task] = list
	}
	return result
}

func loadManifest(path string) (*ManifestSummary, error) {
	// For simplicity, we just parse the fields we need.
	// In production, use the loader package. Here we do a minimal parse.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m ManifestSummary
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func loadFixtures(path string) ([]FixtureInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var fixtures []FixtureInfo
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var f FixtureInfo
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			return nil, err
		}
		fixtures = append(fixtures, f)
	}
	return fixtures, nil
}

func estimateCostMicro(rateIn, rateOut jevledger.MicroUnit, tokensIn, tokensOut int64) jevledger.MicroUnit {
	costIn := (jevledger.MicroUnit(tokensIn)*rateIn + jevledger.MicroUnitsPerUnit - 1) / jevledger.MicroUnitsPerUnit
	costOut := (jevledger.MicroUnit(tokensOut)*rateOut + jevledger.MicroUnitsPerUnit - 1) / jevledger.MicroUnitsPerUnit
	return costIn + costOut
}

func MicroUnitFromBase(base float64) jevledger.MicroUnit {
	if base <= 0 {
		return 0
	}
	if base > float64(jevledger.MaxMicroUnits)/float64(jevledger.MicroUnitsPerUnit) {
		return jevledger.MaxMicroUnits
	}
	// Round UP (ceiling) for conservative cost estimation.
	return jevledger.MicroUnit(math.Ceil(base * float64(jevledger.MicroUnitsPerUnit)))
}

func formatMicro(m jevledger.MicroUnit) string {
	base := float64(m) / float64(jevledger.MicroUnitsPerUnit)
	return fmt.Sprintf("$%.6f", base)
}

func hashString(s string) string {
	h := 0
	for i := 0; i < len(s); i++ {
		h = h*31 + int(s[i])
	}
	return fmt.Sprintf("%08x", h)
}

func writeOutput(path string, plan DryRunPlan, asJSON, verbose bool) error {
	var out *os.File
	if path == "" || path == "-" {
		out = os.Stdout
	} else {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(plan)
	}

	// Human-readable output.
	fmt.Fprintf(out, "=== JEV DRY-RUN COST PROJECTION ===\n")
	fmt.Fprintf(out, "Run ID:        %s\n", plan.RunID)
	fmt.Fprintf(out, "Model:         %s\n", plan.Model)
	fmt.Fprintf(out, "Protocol:      %s\n", plan.ProtocolVersion)
	fmt.Fprintf(out, "Generated:     %s\n", plan.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(out, "Fixture SHA:   %s\n", plan.FixtureSHA)
	fmt.Fprintf(out, "Rates Ver:     %s\n", plan.RatesVersion)
	fmt.Fprintf(out, "Token Bounds:  %s\n", plan.TokenBoundsSHA)
	fmt.Fprintf(out, "Repetitions:   %d\n", plan.Repetitions)
	fmt.Fprintf(out, "\n")
	fmt.Fprintf(out, "Rates:         $%.6f / 1M in, $%.6f / 1M out\n", plan.RateIn, plan.RateOut)
	fmt.Fprintf(out, "Token Limits:  %d in / %d out per attempt\n", plan.MaxInputTokens, plan.MaxOutputTokens)
	fmt.Fprintf(out, "Retries:       %d (x%.2f per retry)\n", plan.MaxRetries, plan.RetryMultiplier)
	fmt.Fprintf(out, "Discovery:     $%.6f (x%.2f)\n", plan.DiscoveryCost, plan.DiscoveryMultiplier)
	fmt.Fprintf(out, "Authorized:    $%.6f\n", plan.AuthorizedCap)
	fmt.Fprintf(out, "\n")

	for _, te := range plan.TaskEstimates {
		fmt.Fprintf(out, "--- %s (%d fixtures, %d attempts) ---\n", te.Task, te.FixtureCount, te.FixtureCount*plan.Repetitions)
		for _, sge := range te.SourceGroups {
			fmt.Fprintf(out, "  %-20s %3d fixtures | %d attempts | base=%s retry=%s disc=%s | TOTAL=%s\n",
				sge.SourceGroup, sge.FixtureCount, sge.Attempts,
				formatMicro(sge.BasePerAttempt),
				formatMicro(sge.RetryBudget),
				formatMicro(sge.DiscoveryBudget),
				formatMicro(sge.TotalReserved))
		}
		fmt.Fprintf(out, "  TASK TOTAL: %s\n\n", formatMicro(te.TotalReserved))
	}

	fmt.Fprintf(out, "==================================\n")
	fmt.Fprintf(out, "GRAND TOTAL RESERVED: %s\n", formatMicro(plan.TotalReserved))
	fmt.Fprintf(out, "AUTHORIZED CAP:       %s\n", formatMicro(MicroUnitFromBase(plan.AuthorizedCap)))
	fmt.Fprintf(out, "AVAILABLE:            %s\n", formatMicro(plan.TotalAvailable))
	if plan.CapExceeded {
		fmt.Fprintf(out, "!!! CAP EXCEEDED !!!\n")
	}
	fmt.Fprintf(out, "\nMISSING APPROVALS:\n")
	for _, a := range plan.MissingApprovals {
		fmt.Fprintf(out, "  - %s\n", a)
	}
	return nil
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "jev-dryrun: "+format+"\n", args...)
	os.Exit(1)
}
