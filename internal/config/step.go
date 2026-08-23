package config

type ModelConfig struct {
	Temperature        float64
	HasTemperature     bool
	MaxOutputTokens    int
	HasMaxOutputTokens bool
}

type Limits struct {
	Timeout          string
	MaxIterations    int
	MaxModelCalls    int
	MaxToolCalls     int
	MaxConcurrency   int
	MaxParallelCalls int
	MaxArtifactBytes int
}

type InstructionKind uint8

const (
	InstructionUnknown InstructionKind = iota
	InstructionText
)

type Instruction struct {
	Kind InstructionKind
	Text string
}

type Schema struct {
	Type       string
	Properties map[string]Schema
	Required   []string
	Items      *Schema
	MaxLength  int
	MaxItems   int
	Enum       []string
	Minimum    float64
	Maximum    float64
	HasMinimum bool
	HasMaximum bool
}

type Input struct {
	Schema Schema
}

type WorkspaceRef struct {
	Name   string
	Access string
}

type BindingKind uint8

const (
	BindingUnknown BindingKind = iota
	BindingInput
	BindingLiteral
)

type Binding struct {
	Kind    BindingKind
	Input   string
	Literal string
}

type Step struct {
	ID          string
	Needs       []string
	Instruction Instruction
	Model       string
	ModelConfig ModelConfig
	Tools       []string
	Workspaces  []WorkspaceRef
	Input       Schema
	With        map[string]Binding
	Output      Schema
	Limits      Limits
}
