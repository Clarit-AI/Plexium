package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/Clarit-AI/Plexium/evaluations/jev/probe"
)

func main() {
	if err := runCLI(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(args []string, out io.Writer) error {
	return runCLIWithConfig(args, out, probe.DefaultRunConfig)
}

type configFactory func(inventoryPath, stateDir, apiKey string) probe.RunConfig
type credentialLookup func(string) (string, bool)

type singleStringFlag struct {
	name  string
	value string
	set   bool
}

func (f *singleStringFlag) String() string { return f.value }

func (f *singleStringFlag) Set(value string) error {
	if f.set {
		return fmt.Errorf("--%s may be provided only once", f.name)
	}
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("--%s value must not begin with '-' (prefix a leading-hyphen path with ./)", f.name)
	}
	f.set = true
	f.value = value
	return nil
}

type singleBoolFlag struct {
	name  string
	value bool
	set   bool
}

func (f *singleBoolFlag) String() string   { return strconv.FormatBool(f.value) }
func (f *singleBoolFlag) IsBoolFlag() bool { return true }

func (f *singleBoolFlag) Set(value string) error {
	if f.set {
		return fmt.Errorf("--%s may be provided only once", f.name)
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("--%s requires a boolean value: %w", f.name, err)
	}
	f.set = true
	f.value = parsed
	return nil
}

func runCLIWithConfig(args []string, out io.Writer, makeConfig configFactory) error {
	return runCLIWithConfigAndCredential(args, out, makeConfig, os.LookupEnv)
}

func runCLIWithConfigAndCredential(args []string, out io.Writer, makeConfig configFactory, lookup credentialLookup) error {
	fs := flag.NewFlagSet("jev-pin-probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := singleBoolFlag{name: "dry-run"}
	execute := singleBoolFlag{name: "execute"}
	deriveAssessment := singleBoolFlag{name: "derive-assessment"}
	inventory := singleStringFlag{name: "inventory", value: "evaluations/jev/pilot/request-inventory.json"}
	stateDir := singleStringFlag{name: "state-dir"}
	requestOrdinals := singleStringFlag{name: "request-ordinals"}
	sourceReports := singleStringFlag{name: "source-reports"}
	assessmentOut := singleStringFlag{name: "assessment-out"}
	fs.Var(&dryRun, "dry-run", "validate and print the four-request plan without reading credentials or dialing")
	fs.Var(&execute, "execute", "execute the bounded one-shot probe")
	fs.Var(&deriveAssessment, "derive-assessment", "derive a corrected offline assessment from immutable probe reports")
	fs.Var(&inventory, "inventory", "accepted frozen request inventory")
	fs.Var(&stateDir, "state-dir", "new private state directory for ledger, report, and raw evidence")
	fs.Var(&requestOrdinals, "request-ordinals", "explicit approved request subset as comma-separated ordinals (for example 2,3,4)")
	fs.Var(&sourceReports, "source-reports", "comma-separated immutable source report paths for offline assessment")
	fs.Var(&assessmentOut, "assessment-out", "new path for the derived assessment; never overwrites a source report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("unexpected positional arguments: %q", fs.Args())
	}
	modes := 0
	for _, selected := range []bool{dryRun.value, execute.value, deriveAssessment.value} {
		if selected {
			modes++
		}
	}
	if modes != 1 {
		return errors.New("exactly one of --dry-run, --execute, or --derive-assessment is required")
	}
	if deriveAssessment.value {
		if stateDir.set || requestOrdinals.set {
			return errors.New("--derive-assessment does not accept --state-dir or --request-ordinals")
		}
		paths, err := parseSourceReports(sourceReports.value)
		if err != nil {
			return err
		}
		if !assessmentOut.set || assessmentOut.value == "" {
			return errors.New("--assessment-out is required for --derive-assessment")
		}
		assessment, err := probe.DeriveAssessmentToFile(inventory.value, paths, assessmentOut.value)
		if err != nil {
			return err
		}
		return writeJSON(out, assessment)
	}
	if sourceReports.set || assessmentOut.set {
		return errors.New("--source-reports and --assessment-out require --derive-assessment")
	}
	var selectedOrdinals []int
	if requestOrdinals.set {
		var err error
		selectedOrdinals, err = parseRequestOrdinals(requestOrdinals.value)
		if err != nil {
			return err
		}
	}
	plan, err := probe.BuildPlan(inventory.value, probe.Endpoints{Jev: probe.JevEndpoint, Nano: probe.NanoEndpoint})
	if err != nil {
		return err
	}
	if _, err := probe.SelectPlanRequests(plan, selectedOrdinals); err != nil {
		return err
	}
	if dryRun.value {
		return writeJSON(out, plan)
	}
	if stateDir.value == "" {
		return errors.New("--state-dir is required for --execute")
	}
	key, present := lookup(probe.CredentialEnv)
	if !present || key == "" {
		return fmt.Errorf("%s is required for --execute", probe.CredentialEnv)
	}
	cfg := makeConfig(inventory.value, stateDir.value, key)
	cfg.RequestOrdinals = append([]int(nil), selectedOrdinals...)
	report, err := probe.Run(context.Background(), cfg)
	if report != nil {
		if writeErr := writeJSON(out, report); writeErr != nil && err == nil {
			return writeErr
		}
	}
	return err
}

func parseSourceReports(raw string) ([]string, error) {
	if raw == "" {
		return nil, errors.New("--source-reports is required for --derive-assessment")
	}
	paths := strings.Split(raw, ",")
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		if path == "" || strings.HasPrefix(path, "-") {
			return nil, errors.New("--source-reports must contain nonempty comma-separated paths that do not begin with '-'")
		}
		if seen[path] {
			return nil, fmt.Errorf("--source-reports contains duplicate path %q", path)
		}
		seen[path] = true
	}
	return paths, nil
}

func parseRequestOrdinals(raw string) ([]int, error) {
	if raw == "" {
		return nil, errors.New("--request-ordinals must not be empty when provided")
	}
	parts := strings.Split(raw, ",")
	ordinals := make([]int, 0, len(parts))
	seen := make(map[int]bool, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, errors.New("--request-ordinals must contain only comma-separated positive integers")
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return nil, errors.New("--request-ordinals must contain only comma-separated positive integers")
			}
		}
		ordinal, err := strconv.Atoi(part)
		if err != nil || ordinal <= 0 {
			return nil, errors.New("--request-ordinals must contain only comma-separated positive integers")
		}
		if seen[ordinal] {
			return nil, fmt.Errorf("--request-ordinals contains duplicate ordinal %d", ordinal)
		}
		seen[ordinal] = true
		ordinals = append(ordinals, ordinal)
	}
	return ordinals, nil
}

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
