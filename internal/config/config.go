package config

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	Version       = 1
	yamlStringTag = "!!str"
	yamlIntTag    = "!!int"
	yamlFloatTag  = "!!float"
	yamlBoolTag   = "!!bool"
)

const (
	CodeInvalidUTF8     = "invalid_utf8"
	CodeInvalidYAML     = "invalid_yaml"
	CodeMultipleDocs    = "multiple_documents"
	CodeUnknownField    = "unknown_field"
	CodeDuplicateKey    = "duplicate_key"
	CodeAlias           = "yaml_alias"
	CodeAnchor          = "yaml_anchor"
	CodeMerge           = "yaml_merge"
	CodeNull            = "yaml_null"
	CodeUnsupportedTag  = "unsupported_tag"
	CodeInvalidType     = "invalid_type"
	CodeInvalidValue    = "invalid_value"
	CodeMissingField    = "missing_field"
	CodeUnsupportedVers = "unsupported_version"
)

type DiagnosticError struct {
	Code   string
	File   string
	Path   string
	Line   int
	Column int
}

func (e *DiagnosticError) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s: %s", e.File, e.Line, e.Column, e.Path, e.Code)
}

type Provider struct {
	Type   string
	Config map[string]string
}

type Model struct {
	Provider string
	Target   string
}

type Evidence struct {
	Directory string
}

type Workspace struct {
	Root   string
	Access string
}

type Config struct {
	Version    int
	Providers  map[string]Provider
	Models     map[string]Model
	Workspaces map[string]Workspace
	Evidence   Evidence
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is selected by the caller
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	return DecodeConfig(path, data)
}

func DecodeConfig(name string, data []byte) (*Config, error) {
	root, err := decodeDocument(name, data)
	if err != nil {
		return nil, err
	}

	fields, err := mappingFields(name, root, "$", "version", "providers", "models", "workspaces", "evidence")
	if err != nil {
		return nil, err
	}

	version, err := requiredInt(name, fields, "version", "$.version")
	if err != nil {
		return nil, err
	}

	if version != Version {
		return nil, diagnostic(name, "$.version", fields["version"], CodeUnsupportedVers)
	}

	providers, err := decodeProviders(name, fields["providers"])
	if err != nil {
		return nil, err
	}

	models, err := decodeModels(name, fields["models"])
	if err != nil {
		return nil, err
	}

	workspaces, err := decodeTrustedWorkspaces(name, fields["workspaces"])
	if err != nil {
		return nil, err
	}

	evidence, err := decodeEvidence(name, fields["evidence"])
	if err != nil {
		return nil, err
	}

	if err := validateConfigReferences(name, providers, models, fields); err != nil {
		return nil, err
	}

	expandProviderConfig(providers)

	for workspaceName, workspace := range workspaces {
		workspace.Root = os.Expand(workspace.Root, os.Getenv)
		workspaces[workspaceName] = workspace
	}

	evidence.Directory = os.Expand(evidence.Directory, os.Getenv)

	return &Config{Version: version, Providers: providers, Models: models, Workspaces: workspaces, Evidence: evidence}, nil
}

func decodeTrustedWorkspaces(name string, node *yaml.Node) (map[string]Workspace, error) {
	if node == nil {
		return map[string]Workspace{}, nil
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, "$.workspaces", node, CodeInvalidType)
	}

	workspaces := make(map[string]Workspace, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]

		path := "$.workspaces." + key.Value
		if !namePattern.MatchString(key.Value) {
			return nil, diagnostic(name, path, key, CodeInvalidValue)
		}

		fields, err := mappingFields(name, value, path, "root", "access")
		if err != nil {
			return nil, err
		}

		root, err := requiredString(name, fields, "root", path+".root")
		if err != nil {
			return nil, err
		}

		access, err := requiredString(name, fields, "access", path+".access")
		if err != nil {
			return nil, err
		}

		if access != "read" {
			return nil, diagnostic(name, path+".access", fields["access"], CodeInvalidValue)
		}

		workspaces[key.Value] = Workspace{Root: root, Access: access}
	}

	return workspaces, nil
}

func decodeEvidence(name string, node *yaml.Node) (Evidence, error) {
	if node == nil {
		return Evidence{}, nil
	}

	fields, err := mappingFields(name, node, "$.evidence", "directory")
	if err != nil {
		return Evidence{}, err
	}

	directory, err := optionalString(name, fields["directory"], "$.evidence.directory")
	if err != nil {
		return Evidence{}, err
	}

	return Evidence{Directory: directory}, nil
}

func decodeProviders(name string, node *yaml.Node) (map[string]Provider, error) {
	if node == nil {
		return nil, diagnostic(name, "$.providers", nil, CodeMissingField)
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, "$.providers", node, CodeInvalidType)
	}

	providers := make(map[string]Provider, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Tag != yamlStringTag {
			return nil, diagnostic(name, "$.providers", key, CodeInvalidType)
		}

		path := "$.providers." + key.Value

		fields, err := mappingFields(name, value, path, "type", "config")
		if err != nil {
			return nil, err
		}

		providerType, err := requiredString(name, fields, "type", path+".type")
		if err != nil {
			return nil, err
		}

		providerConfig, err := decodeStringMap(name, fields["config"], path+".config", true)
		if err != nil {
			return nil, err
		}

		providers[key.Value] = Provider{Type: providerType, Config: providerConfig}
	}

	return providers, nil
}

func decodeModels(name string, node *yaml.Node) (map[string]Model, error) {
	if node == nil {
		return nil, diagnostic(name, "$.models", nil, CodeMissingField)
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, "$.models", node, CodeInvalidType)
	}

	models := make(map[string]Model, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Tag != yamlStringTag {
			return nil, diagnostic(name, "$.models", key, CodeInvalidType)
		}

		path := "$.models." + key.Value

		fields, err := mappingFields(name, value, path, "provider", "target")
		if err != nil {
			return nil, err
		}

		provider, err := requiredString(name, fields, "provider", path+".provider")
		if err != nil {
			return nil, err
		}

		target, err := requiredString(name, fields, "target", path+".target")
		if err != nil {
			return nil, err
		}

		models[key.Value] = Model{Provider: provider, Target: target}
	}

	return models, nil
}

func validateConfigReferences(name string, providers map[string]Provider, models map[string]Model, rootFields map[string]*yaml.Node) error {
	providerNodes := rootFields["providers"]
	for i := 0; i < len(providerNodes.Content); i += 2 {
		key := providerNodes.Content[i]
		if !namePattern.MatchString(key.Value) {
			return diagnostic(name, "$.providers."+key.Value, key, CodeInvalidValue)
		}
	}

	modelNodes := rootFields["models"]
	for i := 0; i < len(modelNodes.Content); i += 2 {
		key := modelNodes.Content[i]
		if !namePattern.MatchString(key.Value) {
			return diagnostic(name, "$.models."+key.Value, key, CodeInvalidValue)
		}

		model := models[key.Value]
		if _, exists := providers[model.Provider]; !exists {
			return diagnostic(name, "$.models."+key.Value+".provider", modelNodes.Content[i+1], CodeInvalidValue)
		}
	}

	return nil
}

func expandProviderConfig(providers map[string]Provider) {
	for providerName, provider := range providers {
		for key, value := range provider.Config {
			provider.Config[key] = os.Expand(value, os.Getenv)
		}

		providers[providerName] = provider
	}
}

func decodeDocument(name string, data []byte) (*yaml.Node, error) {
	if !utf8.Valid(data) {
		return nil, diagnostic(name, "$", nil, CodeInvalidUTF8)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(data)))

	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, &DiagnosticError{Code: CodeInvalidYAML, File: name, Path: "$", Line: 1, Column: 1}
	}

	if len(document.Content) != 1 {
		return nil, diagnostic(name, "$", &document, CodeInvalidYAML)
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, diagnostic(name, "$", &extra, CodeInvalidYAML)
		}

		return nil, diagnostic(name, "$", &extra, CodeMultipleDocs)
	}

	root := document.Content[0]
	if err := validateYAMLNode(name, root, "$"); err != nil {
		return nil, err
	}

	return root, nil
}

func validateYAMLNode(name string, node *yaml.Node, path string) error {
	if node.Anchor != "" {
		return diagnostic(name, path, node, CodeAnchor)
	}

	if node.Kind == yaml.AliasNode {
		return diagnostic(name, path, node, CodeAlias)
	}

	if node.Tag == "!!null" {
		return diagnostic(name, path, node, CodeNull)
	}

	if node.Tag == "!!merge" || node.Value == "<<" {
		return diagnostic(name, path, node, CodeMerge)
	}

	if !supportedTag(node) {
		return diagnostic(name, path, node, CodeUnsupportedTag)
	}

	switch node.Kind {
	case yaml.MappingNode:
		return validateYAMLMapping(name, node, path)
	case yaml.SequenceNode:
		return validateYAMLSequence(name, node, path)
	case yaml.ScalarNode:
		return nil
	default:
		return diagnostic(name, path, node, CodeInvalidYAML)
	}
}

func validateYAMLMapping(name string, node *yaml.Node, path string) error {
	keys := make(map[string]struct{}, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		childPath := path + "." + key.Value

		if key.Tag == "!!merge" || key.Value == "<<" {
			return diagnostic(name, childPath, key, CodeMerge)
		}

		if key.Kind != yaml.ScalarNode || key.Tag != yamlStringTag {
			return diagnostic(name, childPath, key, CodeInvalidType)
		}

		if _, exists := keys[key.Value]; exists {
			return diagnostic(name, childPath, key, CodeDuplicateKey)
		}

		keys[key.Value] = struct{}{}

		if err := validateYAMLNode(name, value, childPath); err != nil {
			return err
		}
	}

	return nil
}

func validateYAMLSequence(name string, node *yaml.Node, path string) error {
	for i, child := range node.Content {
		if err := validateYAMLNode(name, child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
			return err
		}
	}

	return nil
}

func supportedTag(node *yaml.Node) bool {
	switch node.Kind {
	case yaml.MappingNode:
		return node.Tag == "!!map"
	case yaml.SequenceNode:
		return node.Tag == "!!seq"
	case yaml.ScalarNode:
		return node.Tag == yamlStringTag || node.Tag == yamlIntTag || node.Tag == yamlFloatTag || node.Tag == yamlBoolTag
	default:
		return false
	}
}

func mappingFields(name string, node *yaml.Node, path string, allowed ...string) (map[string]*yaml.Node, error) {
	if node == nil {
		return nil, diagnostic(name, path, nil, CodeMissingField)
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		allowedSet[field] = struct{}{}
	}

	fields := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if _, ok := allowedSet[key.Value]; !ok {
			return nil, diagnostic(name, path+"."+key.Value, key, CodeUnknownField)
		}

		fields[key.Value] = value
	}

	return fields, nil
}

func requiredString(name string, fields map[string]*yaml.Node, field, path string) (string, error) {
	node := fields[field]
	if node == nil {
		return "", diagnostic(name, path, nil, CodeMissingField)
	}

	return scalarString(name, node, path)
}

func scalarString(name string, node *yaml.Node, path string) (string, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != yamlStringTag {
		return "", diagnostic(name, path, node, CodeInvalidType)
	}

	return node.Value, nil
}

func requiredInt(name string, fields map[string]*yaml.Node, field, path string) (int, error) {
	node := fields[field]
	if node == nil {
		return 0, diagnostic(name, path, nil, CodeMissingField)
	}

	return scalarInt(name, node, path)
}

func scalarInt(name string, node *yaml.Node, path string) (int, error) {
	if node.Kind != yaml.ScalarNode {
		return 0, diagnostic(name, path, node, CodeInvalidType)
	}

	if node.Tag != yamlIntTag {
		if node.Tag == yamlFloatTag && isDecimalInteger(node.Value) {
			return 0, diagnostic(name, path, node, CodeInvalidValue)
		}

		return 0, diagnostic(name, path, node, CodeInvalidType)
	}

	value, err := strconv.ParseInt(node.Value, 0, 64)
	if err != nil || value > int64(math.MaxInt) || value < int64(math.MinInt) {
		return 0, diagnostic(name, path, node, CodeInvalidValue)
	}

	return int(value), nil
}

func isDecimalInteger(value string) bool {
	value = strings.TrimPrefix(strings.TrimPrefix(value, "+"), "-")
	if value == "" {
		return false
	}

	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}

	return true
}

func scalarFloat(name string, node *yaml.Node, path string) (float64, error) {
	if node.Kind != yaml.ScalarNode || (node.Tag != yamlFloatTag && node.Tag != yamlIntTag) {
		return 0, diagnostic(name, path, node, CodeInvalidType)
	}

	value, err := strconv.ParseFloat(node.Value, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return 0, diagnostic(name, path, node, CodeInvalidValue)
	}

	return value, nil
}

func decodeStringMap(name string, node *yaml.Node, path string, required bool) (map[string]string, error) {
	if node == nil {
		if required {
			return nil, diagnostic(name, path, nil, CodeMissingField)
		}

		return map[string]string{}, nil
	}

	if node.Kind != yaml.MappingNode {
		return nil, diagnostic(name, path, node, CodeInvalidType)
	}

	values := make(map[string]string, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Tag != yamlStringTag {
			return nil, diagnostic(name, path, key, CodeInvalidType)
		}

		decoded, err := scalarString(name, value, path+"."+key.Value)
		if err != nil {
			return nil, err
		}

		values[key.Value] = decoded
	}

	return values, nil
}

func diagnostic(name, path string, node *yaml.Node, code string) *DiagnosticError {
	line, column := 1, 1
	if node != nil {
		line, column = node.Line, node.Column
		if line == 0 {
			line = 1
		}

		if column == 0 {
			column = 1
		}
	}

	return &DiagnosticError{Code: code, File: name, Path: path, Line: line, Column: column}
}
