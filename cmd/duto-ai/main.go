package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

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
	errCommandRequired        = errors.New("command is required")
	errEmptyPayload           = errors.New("empty command payload")
	errInvalidFormat          = errors.New("invalid format")
	errRunUnavailable         = errors.New("workflow execution is unavailable")
	errUnknownCommand         = errors.New("unknown command")
	errWorkflowNeeded         = errors.New("workflow is required")
	errUnknownModelAlias      = errors.New("unknown admitted model alias")
	errUnknownProvider        = errors.New("unknown admitted provider binding")
	errBundledProvider        = errors.New("creating bundled provider")
	errBundledModel           = errors.New("creating bundled model")
	errInputsRequired         = errors.New("workflow inputs require --inputs FILE")
	errInputsStdin            = errors.New("--inputs=- is invalid; stdin is reserved for workflow '-' input")
	errInputsPath             = errors.New("inputs file path is required")
	errInputsRegularFile      = errors.New("inputs file must be a regular file")
	errInputsInvalidUTF8      = errors.New("inputs file is not valid UTF-8")
	errInputsObjectRoot       = errors.New("inputs JSON root must be an object")
	errInputsTrailingDocument = errors.New("inputs JSON has a trailing document")
	errInputsTrailingToken    = errors.New("inputs JSON has a trailing token")
)

type outputFormat string

const (
	formatText outputFormat = "text"
	formatJSON outputFormat = "json"
)

type runWorkflow func(context.Context, *config.Config, *plan.Plan, map[string]any, outputFormat) ([]byte, error)

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
		configPath        string
		formatValue       string
		inputsPath        string
		evidenceDirectory string
	)

	command := &cobra.Command{
		Use:          commandRun + " [--config FILE] [--format text|json] [--inputs FILE] [--evidence-directory DIR] WORKFLOW|-",
		Args:         workflowArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			format, err := parseFormat(formatValue)
			if err != nil {
				return usageError(err)
			}

			cfg, compiled, err := admitRun(configPath, args[0], evidenceDirectory, command.InOrStdin())
			if err != nil {
				return admissionError(err)
			}

			inputs, hasInputsFile, err := loadRunInputs(inputsPath, command.Flags().Changed("inputs"))
			if err != nil {
				return admissionError(err)
			}

			if len(compiled.Snapshot().Workflow.Inputs) != 0 && !hasInputsFile {
				return admissionError(errInputsRequired)
			}

			if dependencies.run == nil {
				return internalError(errRunUnavailable)
			}

			output, err := dependencies.run(command.Context(), cfg, compiled, inputs, format)
			if err != nil {
				exitCode := codeForError(err)
				if len(output) != 0 && (exitCode == exitExecution || exitCode == exitCancelled) {
					if writeErr := writePayload(command.OutOrStdout(), output); writeErr != nil {
						return writeErr
					}
				}

				return err
			}

			return writePayload(command.OutOrStdout(), output)
		},
	}
	addOperationFlags(command, &configPath, &formatValue)
	command.Flags().StringVar(&inputsPath, "inputs", "", "workflow inputs JSON file")
	command.Flags().StringVar(&evidenceDirectory, "evidence-directory", "", "trusted run-only evidence directory override")
	command.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError(err) })

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

func loadRunInputs(path string, changed bool) (inputs map[string]any, loaded bool, err error) {
	if !changed {
		return map[string]any{}, false, nil
	}

	if path == "" {
		return nil, false, errInputsPath
	}

	if path == "-" {
		return nil, false, errInputsStdin
	}

	fileInfo, err := os.Lstat(path)
	if err != nil {
		return nil, false, fmt.Errorf("checking inputs file: %w", err)
	}

	if fileInfo.Mode()&os.ModeSymlink != 0 || !fileInfo.Mode().IsRegular() {
		return nil, false, errInputsRegularFile
	}

	data, err := os.ReadFile(path) //nolint:gosec // trusted by caller contract
	if err != nil {
		return nil, false, fmt.Errorf("reading inputs file: %w", err)
	}

	if !utf8.Valid(data) {
		return nil, false, errInputsInvalidUTF8
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false, fmt.Errorf("decoding inputs JSON: %w", err)
	}

	inputs, ok := value.(map[string]any)
	if !ok {
		return nil, false, errInputsObjectRoot
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, false, errInputsTrailingToken
		}
	} else {
		switch trailing.(type) {
		case map[string]any, []any:
			return nil, false, errInputsTrailingDocument
		default:
			return nil, false, errInputsTrailingToken
		}
	}

	return inputs, true, nil
}

func normalizedInputsForValidation(inputs map[string]any) map[string]any {
	normalized, ok := normalizeJSONValue(inputs).(map[string]any)
	if !ok {
		return map[string]any{}
	}

	return normalized
}

func normalizeJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, nested := range typed {
			clone[key] = normalizeJSONValue(nested)
		}

		return clone
	case []any:
		clone := make([]any, len(typed))
		for index := range typed {
			clone[index] = normalizeJSONValue(typed[index])
		}

		return clone
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return integer
		}

		if decimal, err := typed.Float64(); err == nil {
			return decimal
		}

		return typed.String()
	default:
		return typed
	}
}

func admit(configPath, workflowPath string, stdin io.Reader) (*config.Config, *plan.Plan, error) {
	return admitRun(configPath, workflowPath, "", stdin)
}

func admitRun(configPath, workflowPath, evidenceDirectory string, stdin io.Reader) (*config.Config, *plan.Plan, error) {
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	if evidenceDirectory != "" {
		cfg.Evidence.Directory = evidenceDirectory
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

func runAdmittedWorkflow(ctx context.Context, cfg *config.Config, compiled *plan.Plan, inputs map[string]any, format outputFormat) ([]byte, error) {
	if err := compiler.ValidateInputs(compiled, normalizedInputsForValidation(inputs)); err != nil {
		return nil, executionError(err)
	}

	registry, err := buildToolRegistry(cfg, compiled)
	if err != nil {
		return nil, executionError(err)
	}

	result, err := runtime.RunWithInputsAndToolsets(ctx, compiled, bundledModelResolver(cfg), registry.FilteredToolset, inputs)

	output, encodeErr := formatRunResult(result, format)
	if encodeErr != nil {
		return nil, internalError(encodeErr)
	}

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return output, context.Canceled
		}

		return output, executionError(err)
	}

	return output, nil
}

func formatRunResult(result *runtime.Result, format outputFormat) ([]byte, error) {
	if result == nil {
		return nil, nil
	}

	if format == formatJSON {
		output, err := result.JSON()
		if err != nil {
			return nil, fmt.Errorf("encoding JSON result: %w", err)
		}

		return output, nil
	}

	output, err := result.Text()
	if err != nil {
		return nil, fmt.Errorf("encoding text result: %w", err)
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
