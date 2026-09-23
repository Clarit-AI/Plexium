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

func runCLIWithConfig(args []string, out io.Writer, makeConfig configFactory) error {
	return runCLIWithConfigAndCredential(args, out, makeConfig, os.LookupEnv)
}

func runCLIWithConfigAndCredential(args []string, out io.Writer, makeConfig configFactory, lookup credentialLookup) error {
	fs := flag.NewFlagSet("jev-pin-probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", false, "validate and print the four-request plan without reading credentials or dialing")
	execute := fs.Bool("execute", false, "execute the bounded one-shot probe")
	inventory := fs.String("inventory", "evaluations/jev/pilot/request-inventory.json", "accepted frozen request inventory")
	stateDir := fs.String("state-dir", "", "new private state directory for ledger, report, and raw evidence")
	requestOrdinalsRaw := fs.String("request-ordinals", "", "explicit approved request subset as comma-separated ordinals (for example 2,3,4)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *execute {
		return errors.New("exactly one of --dry-run or --execute is required")
	}
	selectionProvided := false
	fs.Visit(func(current *flag.Flag) {
		if current.Name == "request-ordinals" {
			selectionProvided = true
		}
	})
	var requestOrdinals []int
	if selectionProvided {
		var err error
		requestOrdinals, err = parseRequestOrdinals(*requestOrdinalsRaw)
		if err != nil {
			return err
		}
	}
	plan, err := probe.BuildPlan(*inventory, probe.Endpoints{Jev: probe.JevEndpoint, Nano: probe.NanoEndpoint})
	if err != nil {
		return err
	}
	if _, err := probe.SelectPlanRequests(plan, requestOrdinals); err != nil {
		return err
	}
	if *dryRun {
		return writeJSON(out, plan)
	}
	if *stateDir == "" {
		return errors.New("--state-dir is required for --execute")
	}
	key, present := lookup(probe.CredentialEnv)
	if !present || key == "" {
		return fmt.Errorf("%s is required for --execute", probe.CredentialEnv)
	}
	cfg := makeConfig(*inventory, *stateDir, key)
	cfg.RequestOrdinals = append([]int(nil), requestOrdinals...)
	report, err := probe.Run(context.Background(), cfg)
	if report != nil {
		if writeErr := writeJSON(out, report); writeErr != nil && err == nil {
			return writeErr
		}
	}
	return err
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
