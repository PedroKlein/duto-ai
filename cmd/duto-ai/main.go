package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/PedroKlein/duto-ai/internal/runtime"
)

const defaultConfigPath = ".github/ai-workflows/config.yaml"

// Set by goreleaser ldflags.
var (
	version = "dev"
	commit  = "none"
)

// ErrMissingWorkflowPath is returned when no workflow file is specified.
var ErrMissingWorkflowPath = errors.New("workflow YAML path is required")

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: duto-ai run [--config path] <workflow.yaml>\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		if err := runCommand(os.Args[2:]); err != nil {
			log.Fatalf("Error: %v", err)
		}
	case "version":
		fmt.Printf("duto-ai %s (%s)\n", version, commit)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)

	configPath := fs.String("config", defaultConfigPath, "path to config.yaml")
	logLevel := fs.String("log-level", "info", "log level (debug/info/error)")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	if fs.NArg() < 1 {
		return ErrMissingWorkflowPath
	}

	workflowPath := fs.Arg(0)

	// Configure log level
	switch *logLevel {
	case "error":
		log.SetOutput(os.Stderr)
	case "debug":
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	default:
		// info level - default
	}

	ctx := context.Background()

	if err := runtime.Run(ctx, *configPath, workflowPath); err != nil {
		return fmt.Errorf("running workflow: %w", err)
	}

	return nil
}
