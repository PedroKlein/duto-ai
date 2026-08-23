package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"google.golang.org/adk/v2/model"

	"github.com/PedroKlein/duto-ai/internal/compiler"
	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/plan"
	"github.com/PedroKlein/duto-ai/internal/runtime"
)

const defaultConfigPath = "duto.yaml"

const (
	commandValidate = "validate"
	commandPlan     = "plan"
	commandRun      = "run"
	commandVersion  = "version"
)

const (
	exitSuccess   = 0
	exitInternal  = 1
	exitUsage     = 2
	exitAdmission = 3
	exitExecution = 4
	exitCancelled = 130
)

var (
	version = "dev"
	commit  = "none"
)

var (
	errCommandRequired   = errors.New("command is required")
	errEmptyPayload      = errors.New("empty command payload")
	errInvalidFormat     = errors.New("invalid format")
	errRunUnavailable    = errors.New("workflow execution is unavailable")
	errUnknownCommand    = errors.New("unknown command")
	errWorkflowNeeded    = errors.New("workflow is required")
	errUnknownModelAlias = errors.New("unknown admitted model alias")
	errUnknownProvider   = errors.New("unknown admitted provider binding")
	errBundledProvider   = errors.New("creating bundled provider")
	errBundledModel      = errors.New("creating bundled model")
	errWorkflowInputs    = errors.New("workflow requires host-supplied inputs unavailable in the CLI")
)

type outputFormat string

const (
	formatText outputFormat = "text"
	formatJSON outputFormat = "json"
)

type runWorkflow func(context.Context, *config.Config, *plan.Plan, outputFormat) ([]byte, error)

type commandDependencies struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	run    runWorkflow
}

type commandError struct {
	code int
	err  error
}

func (e *commandError) Error() string {
	return e.err.Error()
}

func (e *commandError) Unwrap() error {
	return e.err
}

func main() {
	dependencies := commandDependencies{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		run:    runAdmittedWorkflow,
	}
	os.Exit(execute(context.Background(), os.Args[1:], dependencies))
}

func execute(ctx context.Context, args []string, dependencies commandDependencies) int {
	commands := []string{commandValidate, commandPlan, commandRun, commandVersion}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") && !slices.Contains(commands, args[0]) {
		err := fmt.Errorf("%w %q for \"duto-ai\"", errUnknownCommand, args[0])

		return writeError(dependencies.stderr, usageError(err))
	}

	root := newRootCommand(dependencies)
	root.SetArgs(args)

	if err := root.ExecuteContext(ctx); err != nil {
		return writeError(dependencies.stderr, err)
	}

	return exitSuccess
}

func newRootCommand(dependencies commandDependencies) *cobra.Command {
	root := &cobra.Command{
		Use:           "duto-ai",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return usageError(errCommandRequired)
		},
	}
	root.SetIn(dependencies.stdin)
	root.SetOut(dependencies.stdout)
	root.SetErr(dependencies.stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.AddCommand(
		newOperationCommand(commandValidate, validatePayload),
		newOperationCommand(commandPlan, planPayload),
		newRunCommand(dependencies),
		newVersionCommand(dependencies.stdout),
	)

	return root
}

type admissionPayload func(*plan.Plan, outputFormat) ([]byte, error)

func newOperationCommand(name string, payload admissionPayload) *cobra.Command {
	var (
		configPath  string
		formatValue string
	)

	command := &cobra.Command{
		Use:          name + " [--config FILE] [--format text|json] WORKFLOW|-",
		Args:         workflowArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			format, err := parseFormat(formatValue)
			if err != nil {
				return usageError(err)
			}

			_, compiled, err := admit(configPath, args[0], command.InOrStdin())
			if err != nil {
				return admissionError(err)
			}

			output, err := payload(compiled, format)
			if err != nil {
				return internalError(err)
			}

			return writePayload(command.OutOrStdout(), output)
		},
	}
	addOperationFlags(command, &configPath, &formatValue)

	return command
}

func newRunCommand(dependencies commandDependencies) *cobra.Command {
	var (
		configPath  string
		formatValue string
	)

	command := &cobra.Command{
		Use:          commandRun + " [--config FILE] [--format text|json] WORKFLOW|-",
		Args:         workflowArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			format, err := parseFormat(formatValue)
			if err != nil {
				return usageError(err)
			}

			cfg, compiled, err := admit(configPath, args[0], command.InOrStdin())
			if err != nil {
				return admissionError(err)
			}

			if dependencies.run == nil {
				return internalError(errRunUnavailable)
			}

			output, err := dependencies.run(command.Context(), cfg, compiled, format)
			if err != nil {
				return err
			}

			return writePayload(command.OutOrStdout(), output)
		},
	}
	addOperationFlags(command, &configPath, &formatValue)

	return command
}

func newVersionCommand(output io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:  commandVersion,
		Args: noArgs,
		RunE: func(*cobra.Command, []string) error {
			if _, err := fmt.Fprintf(output, "duto-ai %s (%s)\n", version, commit); err != nil {
				return internalError(fmt.Errorf("writing version: %w", err))
			}

			return nil
		},
	}
}

func addOperationFlags(command *cobra.Command, configPath, formatValue *string) {
	command.Flags().StringVar(configPath, "config", defaultConfigPath, "trusted runtime configuration")
	command.Flags().StringVar(formatValue, "format", string(formatText), "output format: text or json")
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })
}

func workflowArgs(command *cobra.Command, args []string) error {
	if len(args) == 0 {
		return usageError(errWorkflowNeeded)
	}

	if err := cobra.ExactArgs(1)(command, args); err != nil {
		return usageError(err)
	}

	return nil
}

func noArgs(command *cobra.Command, args []string) error {
	if err := cobra.NoArgs(command, args); err != nil {
		return usageError(err)
	}

	return nil
}

func parseFormat(value string) (outputFormat, error) {
	switch outputFormat(value) {
	case formatText:
		return formatText, nil
	case formatJSON:
		return formatJSON, nil
	default:
		return "", fmt.Errorf("%w %q", errInvalidFormat, value)
	}
}

func admit(configPath, workflowPath string, stdin io.Reader) (*config.Config, *plan.Plan, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	workflow, err := loadWorkflow(workflowPath, stdin)
	if err != nil {
		return nil, nil, err
	}

	compiled, err := plan.Compile(cfg, workflow)
	if err != nil {
		return nil, nil, fmt.Errorf("compiling plan: %w", err)
	}

	return cfg, compiled, nil
}

func loadWorkflow(path string, stdin io.Reader) (*config.Workflow, error) {
	if path != "-" {
		workflow, err := config.LoadWorkflow(path)
		if err != nil {
			return nil, fmt.Errorf("loading workflow: %w", err)
		}

		return workflow, nil
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("reading workflow stdin: %w", err)
	}

	workflow, err := config.DecodeWorkflow("<stdin>", data)
	if err != nil {
		return nil, fmt.Errorf("loading workflow: %w", err)
	}

	return workflow, nil
}

func runAdmittedWorkflow(ctx context.Context, cfg *config.Config, compiled *plan.Plan, format outputFormat) ([]byte, error) {
	if len(compiled.Snapshot().Workflow.Inputs) != 0 {
		return nil, executionError(errWorkflowInputs)
	}

	registry, err := buildToolRegistry(cfg, compiled)
	if err != nil {
		return nil, executionError(err)
	}

	result, err := runtime.RunWithInputsAndToolsets(ctx, compiled, bundledModelResolver(cfg), registry.FilteredToolset, map[string]any{})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}

		return nil, executionError(err)
	}

	if format == formatJSON {
		output, encodeErr := result.JSON()
		if encodeErr != nil {
			return nil, internalError(encodeErr)
		}

		return output, nil
	}

	output, err := result.Text()
	if err != nil {
		return nil, internalError(err)
	}

	return output, nil
}

func bundledModelResolver(cfg *config.Config) compiler.ModelResolver {
	var mu sync.Mutex

	providers := make(map[string]*bundledProvider)
	models := make(map[string]model.LLM)

	return func(ctx context.Context, alias string) (model.LLM, error) {
		mu.Lock()
		defer mu.Unlock()

		if llm := models[alias]; llm != nil {
			return llm, nil
		}

		binding, exists := cfg.Models[alias]
		if !exists {
			return nil, errUnknownModelAlias
		}

		bundled := providers[binding.Provider]
		if bundled == nil {
			definition, ok := cfg.Providers[binding.Provider]
			if !ok {
				return nil, errUnknownProvider
			}

			created, err := newBundledProvider(ctx, definition)
			if err != nil {
				return nil, errBundledProvider
			}

			bundled = created
			providers[binding.Provider] = bundled
		}

		llm, err := bundled.model(binding.Target)
		if err != nil {
			return nil, errBundledModel
		}

		models[alias] = llm

		return llm, nil
	}
}

func validatePayload(_ *plan.Plan, format outputFormat) ([]byte, error) {
	if format == formatJSON {
		return []byte(`{"version":1,"valid":true}`), nil
	}

	return []byte("valid"), nil
}

func planPayload(compiled *plan.Plan, format outputFormat) ([]byte, error) {
	if format == formatJSON {
		return compiled.JSON(), nil
	}

	return compiled.Text(), nil
}

func writePayload(output io.Writer, payload []byte) error {
	payload = bytes.TrimRight(payload, "\n")
	if len(payload) == 0 {
		return internalError(errEmptyPayload)
	}

	if _, err := fmt.Fprintf(output, "%s\n", payload); err != nil {
		return internalError(fmt.Errorf("writing command output: %w", err))
	}

	return nil
}

func writeError(output io.Writer, err error) int {
	if _, writeErr := fmt.Fprintf(output, "error: %v\n", err); writeErr != nil {
		return exitInternal
	}

	return codeForError(err)
}

func codeForError(err error) int {
	if errors.Is(err, context.Canceled) {
		return exitCancelled
	}

	var commandErr *commandError
	if errors.As(err, &commandErr) {
		return commandErr.code
	}

	return exitInternal
}

func usageError(err error) error {
	return &commandError{code: exitUsage, err: err}
}

func admissionError(err error) error {
	return &commandError{code: exitAdmission, err: err}
}

func executionError(err error) error {
	return &commandError{code: exitExecution, err: err}
}

func internalError(err error) error {
	return &commandError{code: exitInternal, err: err}
}
