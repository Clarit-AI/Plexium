package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Clarit-AI/Plexium/evaluations/jev/probe"
)

func main() {
	if err := runCLI(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCLI(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("jev-pin-probe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dryRun := fs.Bool("dry-run", false, "validate and print the four-request plan without reading credentials or dialing")
	execute := fs.Bool("execute", false, "execute the bounded one-shot probe")
	inventory := fs.String("inventory", "evaluations/jev/pilot/request-inventory.json", "accepted frozen request inventory")
	stateDir := fs.String("state-dir", "", "new private state directory for ledger, report, and raw evidence")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dryRun == *execute {
		return errors.New("exactly one of --dry-run or --execute is required")
	}
	plan, err := probe.BuildPlan(*inventory, probe.Endpoints{Jev: probe.JevEndpoint, Nano: probe.NanoEndpoint})
	if err != nil {
		return err
	}
	if *dryRun {
		return writeJSON(out, plan)
	}
	if *stateDir == "" {
		return errors.New("--state-dir is required for --execute")
	}
	key := os.Getenv(probe.CredentialEnv)
	if key == "" {
		return fmt.Errorf("%s is required for --execute", probe.CredentialEnv)
	}
	report, err := probe.Run(context.Background(), probe.DefaultRunConfig(*inventory, *stateDir, key))
	if report != nil {
		if writeErr := writeJSON(out, report); writeErr != nil && err == nil {
			return writeErr
		}
	}
	return err
}

func writeJSON(out io.Writer, value any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}
