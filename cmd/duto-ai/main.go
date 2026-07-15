package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/logging"
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
			slog.Error("fatal", "error", err)
			os.Exit(1)
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
	logLevel := fs.String("log-level", "info", "log level (debug/info/warn/error)")
	repo := fs.String("repo", "", "repository (owner/repo) — overrides GITHUB_REPOSITORY")
	pr := fs.Int("pr", 0, "pull request number — overrides GITHUB_PR_NUMBER")
	event := fs.String("event", "", "path to event.json — overrides GITHUB_EVENT_PATH")
	dryRun := fs.Bool("dry-run", false, "validate and show execution plan without calling LLM")
	outputFormat := fs.String("output-format", "text", "output format (text/json/markdown)")
	outputFile := fs.String("output-file", "", "write output to file (in addition to stdout)")
	verbose := fs.Bool("verbose", false, "enable debug-level output for troubleshooting")

	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	if fs.NArg() < 1 {
		return ErrMissingWorkflowPath
	}

	workflowPath := fs.Arg(0)

	logging.Setup(*logLevel)

	if *verbose {
		logging.Level.Set(slog.LevelDebug)
	}

	// Override env vars from CLI flags for local testing.
	setEnvOverrides(*repo, *pr, *event)

	if *dryRun {
		return dryRunWorkflow(*configPath, workflowPath)
	}

	format, err := runtime.ParseOutputFormat(*outputFormat)
	if err != nil {
		return fmt.Errorf("parsing output format: %w", err)
	}

	ctx := context.Background()

	result, runErr := runtime.RunWithResult(ctx, *configPath, workflowPath)

	// Write output even on failure (partial results are useful).
	if result != nil {
		if wErr := writeOutput(result, format, *outputFile); wErr != nil {
			slog.Error("writing output", "error", wErr)
		}
	}

	if runErr != nil {
		return fmt.Errorf("running workflow: %w", runErr)
	}

	return nil
}

// setEnvOverrides applies CLI flag values as env var overrides.
func setEnvOverrides(repo string, pr int, event string) {
	if repo != "" {
		_ = os.Setenv("GITHUB_REPOSITORY", repo)
	}

	if pr > 0 {
		_ = os.Setenv("GITHUB_PR_NUMBER", strconv.Itoa(pr))
	}

	if event != "" {
		_ = os.Setenv("GITHUB_EVENT_PATH", event)
	}
}

// dryRunWorkflow validates config/workflow and prints the execution plan.
func dryRunWorkflow(configPath, workflowPath string) error {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	wf, err := config.LoadWorkflow(workflowPath)
	if err != nil {
		return fmt.Errorf("loading workflow: %w", err)
	}

	if vErr := config.ValidateWorkflow(wf); vErr != nil {
		return fmt.Errorf("validating workflow: %w", vErr)
	}

	sorted, err := config.TopologicalSort(wf.Steps)
	if err != nil {
		return fmt.Errorf("sorting steps: %w", err)
	}

	fmt.Printf("Workflow: %s\n", wf.Name)
	fmt.Printf("Model:   %s\n", cfg.Defaults.Model)
	fmt.Printf("Steps:   %d\n\n", len(sorted))

	for i, step := range sorted {
		model := step.Model
		if model == "" {
			model = cfg.Defaults.Model
		}

		deps := "none"
		if len(step.Needs) > 0 {
			deps = strings.Join(step.Needs, ", ")
		}

		fmt.Printf("  %d. %s (model: %s, depends: %s)\n", i+1, step.ID, model, deps)
	}

	fmt.Println("\n✓ Validation passed — ready to run.")

	return nil
}

func writeOutput(result *runtime.WorkflowResult, format runtime.OutputFormat, outputFile string) error {
	formatted, err := result.FormatOutput(format)
	if err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	// Write to stdout for JSON (machine-parseable), stderr for text.
	switch format {
	case runtime.OutputFormatJSON:
		_, _ = fmt.Fprintln(os.Stdout, formatted)
	case runtime.OutputFormatText:
		_, _ = fmt.Fprintln(os.Stderr, formatted)
	case runtime.OutputFormatMarkdown:
		_, _ = fmt.Fprintln(os.Stdout, formatted)
	}

	// Write to file if requested.
	if outputFile != "" {
		if wErr := os.WriteFile(outputFile, []byte(formatted+"\n"), 0o644); wErr != nil { //nolint:gosec // output file permissions are intentionally world-readable
			return fmt.Errorf("writing output file %q: %w", outputFile, wErr)
		}
	}

	// Set GitHub step outputs when running in GHA.
	writeGitHubOutput(result)

	return nil
}

func writeGitHubOutput(result *runtime.WorkflowResult) {
	ghOutput := os.Getenv("GITHUB_OUTPUT")
	if ghOutput == "" {
		return
	}

	f, err := os.OpenFile(ghOutput, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644) //nolint:gosec // GITHUB_OUTPUT path is provided by GHA runner
	if err != nil {
		slog.Debug("opening GITHUB_OUTPUT", "error", err)

		return
	}
	defer f.Close() //nolint:errcheck // best-effort write to GHA output

	_, _ = fmt.Fprintf(f, "status=%s\n", result.Status)
	_, _ = fmt.Fprintf(f, "workflow=%s\n", result.WorkflowName)
	_, _ = fmt.Fprintf(f, "duration_ms=%d\n", result.Duration.Milliseconds())

	if failed := result.Failed(); failed != nil {
		_, _ = fmt.Fprintf(f, "failed_step=%s\n", failed.StepID)
	}
}
