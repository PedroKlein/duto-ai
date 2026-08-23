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
	InstructionFile
	InstructionTemplate
	InstructionTemplateFile
)

type FileSource struct {
	Workspace string
	Path      string
	MaxBytes  int
}

type Instruction struct {
	Kind           InstructionKind
	Text           string
	File           FileSource
	MaxOutputBytes int
}

type SkillSource struct {
	Workspace string
	Path      string
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
	BindingOutput
	BindingLiteral
)

type OutputRef struct {
	Step string
	Path []string
}

type Binding struct {
	Kind     BindingKind
	Input    string
	Output   OutputRef
	Literal  any
	Optional bool
}

type Condition struct {
	Step     string
	Outcomes []string
}

type Step struct {
	ID          string
	Needs       []string
	Wait        string
	When        []Condition
	Instruction Instruction
	Model       string
	ModelConfig ModelConfig
	Tools       []string
	Skills      []string
	Workspaces  []WorkspaceRef
	Input       Schema
	With        map[string]Binding
	WithOrder   []string
	Output      Schema
	Limits      Limits
}
