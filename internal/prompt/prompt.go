package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf8"
)

const (
	KindText         = "text"
	KindFile         = "file"
	KindTemplate     = "template"
	KindTemplateFile = "template_file"

	maxInlineBytes      = 64 << 10
	maxFileBytes        = 1 << 20
	maxFinalPromptBytes = 256 << 10
)

var (
	ErrInvalidSource    = errors.New("invalid instruction source")
	ErrUnknownWorkspace = errors.New("unknown workspace")
	ErrSourceBounds     = errors.New("instruction source exceeds bounds")
	ErrInvalidTemplate  = errors.New("invalid instruction template")
	ErrRenderBounds     = errors.New("rendered instruction exceeds bounds")
)

type FileSource struct {
	Workspace string `json:"workspace"`
	Path      string `json:"path"`
	MaxBytes  int    `json:"max_bytes"`
}

type Source struct {
	Kind           string
	Text           string
	File           FileSource
	MaxOutputBytes int
}

type Frozen struct {
	Kind           string   `json:"kind"`
	Workspace      string   `json:"workspace,omitempty"`
	Path           string   `json:"path,omitempty"`
	Source         string   `json:"source"`
	Digest         string   `json:"digest"`
	MaxSourceBytes int      `json:"max_source_bytes"`
	MaxOutputBytes int      `json:"max_output_bytes"`
	Dependencies   []string `json:"dependencies,omitempty"`
	DirectValues   []string `json:"direct_values,omitempty"`
	Rendering      string   `json:"render"`
}

type Data struct {
	Workflow     WorkflowData   `json:"Workflow"`
	Step         StepData       `json:"Step"`
	Predecessors map[string]any `json:"Predecessors"`
	Runtime      RuntimeData    `json:"Runtime"`
}

type WorkflowData struct {
	Name   string         `json:"Name"`
	Inputs map[string]any `json:"Inputs"`
}

type StepData struct {
	ID     string         `json:"ID"`
	Inputs map[string]any `json:"Inputs"`
}

type RuntimeData struct {
	RunID   string `json:"RunID"`
	Attempt int    `json:"Attempt"`
}

func Admit(source Source, workspaces map[string]string) (Frozen, error) {
	body, symbolic, maxSource, err := sourceBytes(source, workspaces)
	if err != nil {
		return Frozen{}, err
	}

	if !utf8.Valid(body) {
		return Frozen{}, fmt.Errorf("validating instruction source: %w", ErrInvalidSource)
	}

	maxOutput := source.MaxOutputBytes
	if source.Kind == KindText || source.Kind == KindFile {
		maxOutput = len(body)
	}

	if maxOutput <= 0 || maxOutput > maxFinalPromptBytes {
		return Frozen{}, fmt.Errorf("validating instruction output bound: %w", ErrSourceBounds)
	}

	dependencies := []string{}
	directValues := []string{}
	render := "static"

	if source.Kind == KindTemplate || source.Kind == KindTemplateFile {
		dependencies, directValues, err = validateTemplate(string(body))
		if err != nil {
			return Frozen{}, err
		}

		render = "deferred"
	}

	digest := sha256.Sum256(body)

	return Frozen{
		Kind:           source.Kind,
		Workspace:      symbolic.Workspace,
		Path:           symbolic.Path,
		Source:         string(body),
		Digest:         hex.EncodeToString(digest[:]),
		MaxSourceBytes: maxSource,
		MaxOutputBytes: maxOutput,
		Dependencies:   dependencies,
		DirectValues:   directValues,
		Rendering:      render,
	}, nil
}

func (f Frozen) Render(data Data) (string, error) {
	if f.Kind == KindText || f.Kind == KindFile {
		return f.Source, nil
	}

	if f.Kind != KindTemplate && f.Kind != KindTemplateFile {
		return "", ErrInvalidSource
	}

	if err := validateDirectValues(data, f.DirectValues); err != nil {
		return "", err
	}

	return renderTemplate(f.Source, data, f.MaxOutputBytes)
}

func sourceBytes(source Source, workspaces map[string]string) (body []byte, symbolic FileSource, maxBytes int, err error) {
	switch source.Kind {
	case KindText, KindTemplate:
		if len(source.Text) > maxInlineBytes {
			return nil, FileSource{}, 0, ErrSourceBounds
		}

		return []byte(source.Text), FileSource{}, maxInlineBytes, nil
	case KindFile, KindTemplateFile:
		body, err := readFile(workspaces, source.File)
		if err != nil {
			return nil, FileSource{}, 0, err
		}

		symbolic := source.File
		symbolic.Path = filepath.ToSlash(filepath.Clean(symbolic.Path))

		return body, symbolic, source.File.MaxBytes, nil
	default:
		return nil, FileSource{}, 0, ErrInvalidSource
	}
}

func readFile(workspaces map[string]string, source FileSource) ([]byte, error) {
	rootPath, ok := workspaces[source.Workspace]
	if !ok || rootPath == "" {
		return nil, fmt.Errorf("resolving instruction workspace: %w", ErrUnknownWorkspace)
	}

	if source.Path == "" || source.MaxBytes <= 0 || source.MaxBytes > maxFileBytes || filepath.IsAbs(source.Path) {
		return nil, ErrInvalidSource
	}

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("opening instruction workspace: %w", err)
	}
	defer func() { _ = root.Close() }()

	info, err := root.Stat(source.Path)
	if err != nil {
		return nil, fmt.Errorf("stating instruction file: %w", err)
	}

	if !info.Mode().IsRegular() || info.Size() > int64(source.MaxBytes) {
		return nil, ErrInvalidSource
	}

	file, err := root.Open(source.Path)
	if err != nil {
		return nil, fmt.Errorf("opening instruction file: %w", err)
	}
	defer func() { _ = file.Close() }()

	body, err := io.ReadAll(io.LimitReader(file, int64(source.MaxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("reading instruction file: %w", err)
	}

	if len(body) > source.MaxBytes {
		return nil, ErrSourceBounds
	}

	return slices.Clone(body), nil
}
