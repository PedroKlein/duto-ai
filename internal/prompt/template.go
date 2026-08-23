package prompt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"text/template/parse"
)

const (
	maxTemplateNodes      = 512
	maxTemplateActions    = 128
	maxNamedTemplates     = 32
	maxTemplateDepth      = 32
	maxTemplateOperations = 10_000
)

func validateTemplate(source string) (dependencies, directValues []string, err error) {
	parsed, err := template.New("instruction").Option("missingkey=error").Funcs(templateFunctions(nil)).Parse(source)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing instruction template: %w", ErrInvalidTemplate)
	}

	if len(parsed.Templates()) > maxNamedTemplates {
		return nil, nil, ErrInvalidTemplate
	}

	inspection := templateInspection{
		calls:        make(map[string][]string),
		dependencies: make(map[string]struct{}),
		directValues: make(map[string]struct{}),
	}

	for _, named := range parsed.Templates() {
		if named.Tree == nil || named.Root == nil {
			continue
		}

		inspection.current = named.Name()
		if err := inspection.walk(named.Root, 0); err != nil {
			return nil, nil, err
		}
	}

	if inspection.nodes > maxTemplateNodes || inspection.actions > maxTemplateActions || inspection.cyclic() {
		return nil, nil, ErrInvalidTemplate
	}

	dependencies = mapKeys(inspection.dependencies)
	directValues = mapKeys(inspection.directValues)

	return dependencies, directValues, nil
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

type templateInspection struct {
	current      string
	nodes        int
	actions      int
	calls        map[string][]string
	dependencies map[string]struct{}
	directValues map[string]struct{}
}

func (i *templateInspection) walk(node parse.Node, depth int) error {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Pointer && reflect.ValueOf(node).IsNil()) {
		return nil
	}

	if depth > maxTemplateDepth {
		return ErrInvalidTemplate
	}

	i.nodes++

	if handled, err := i.walkTerminal(node, depth); handled {
		return err
	}

	switch typed := node.(type) {
	case *parse.ListNode:
		return i.walkNodes(typed.Nodes, depth)
	case *parse.ActionNode:
		i.actions++
		i.recordDirectValues(typed.Pipe)

		return i.walk(typed.Pipe, depth+1)
	case *parse.IfNode:
		i.actions++
		return i.walkBranch(typed.Pipe, typed.List, typed.ElseList, depth)
	case *parse.RangeNode:
		i.actions++
		return i.walkBranch(typed.Pipe, typed.List, typed.ElseList, depth)
	case *parse.WithNode:
		i.actions++
		return i.walkBranch(typed.Pipe, typed.List, typed.ElseList, depth)
	case *parse.TemplateNode:
		i.actions++
		i.calls[i.current] = append(i.calls[i.current], typed.Name)

		return i.walk(typed.Pipe, depth+1)
	case *parse.PipeNode:
		return i.walkPipe(typed, depth)
	case *parse.CommandNode:
		return i.walkNodes(typed.Args, depth)
	default:
		return ErrInvalidTemplate
	}
}

func (i *templateInspection) walkTerminal(node parse.Node, depth int) (bool, error) {
	switch typed := node.(type) {
	case *parse.IdentifierNode:
		return true, validateIdentifier(typed.Ident)
	case *parse.FieldNode:
		i.addDependency(typed.Ident)
		return true, nil
	case *parse.ChainNode:
		return true, i.walkChain(typed, depth)
	case *parse.VariableNode, *parse.DotNode, *parse.TextNode, *parse.StringNode,
		*parse.NumberNode, *parse.BoolNode, *parse.NilNode, *parse.CommentNode,
		*parse.BreakNode, *parse.ContinueNode:
		return true, nil
	default:
		return false, nil
	}
}

func (i *templateInspection) walkNodes(nodes []parse.Node, depth int) error {
	for _, node := range nodes {
		if err := i.walk(node, depth+1); err != nil {
			return err
		}
	}

	return nil
}

func (i *templateInspection) walkPipe(pipe *parse.PipeNode, depth int) error {
	declarations := make([]parse.Node, len(pipe.Decl))
	for index, declaration := range pipe.Decl {
		declarations[index] = declaration
	}

	if err := i.walkNodes(declarations, depth); err != nil {
		return err
	}

	commands := make([]parse.Node, len(pipe.Cmds))
	for index, command := range pipe.Cmds {
		commands[index] = command
	}

	return i.walkNodes(commands, depth)
}

func (i *templateInspection) walkChain(chain *parse.ChainNode, depth int) error {
	if err := i.walk(chain.Node, depth+1); err != nil {
		return err
	}

	i.addDependency(chain.Field)

	return nil
}

func (i *templateInspection) addDependency(parts []string) {
	if len(parts) > 0 {
		i.dependencies["."+joinPath(parts)] = struct{}{}
	}
}

func (i *templateInspection) recordDirectValues(pipe *parse.PipeNode) {
	if pipelineUsesJSON(pipe) {
		return
	}

	for _, command := range pipe.Cmds {
		for _, argument := range command.Args {
			if field, ok := argument.(*parse.FieldNode); ok && len(field.Ident) > 0 {
				i.directValues["."+joinPath(field.Ident)] = struct{}{}
			}
		}
	}
}

func pipelineUsesJSON(pipe *parse.PipeNode) bool {
	if len(pipe.Cmds) == 0 {
		return false
	}

	last := pipe.Cmds[len(pipe.Cmds)-1]
	if len(last.Args) == 0 {
		return false
	}

	identifier, ok := last.Args[0].(*parse.IdentifierNode)

	return ok && identifier.Ident == "json"
}

func validateDirectValues(data Data, paths []string) error {
	for _, valuePath := range paths {
		value, ok := valueAtPath(reflect.ValueOf(data), strings.Split(strings.TrimPrefix(valuePath, "."), "."))
		if !ok {
			continue
		}

		value = indirectValue(value)
		switch value.Kind() {
		case reflect.Array, reflect.Map, reflect.Slice, reflect.Struct:
			return fmt.Errorf("direct interpolation of %s: %w", valuePath, ErrInvalidTemplate)
		default:
			continue
		}
	}

	return nil
}

func valueAtPath(value reflect.Value, parts []string) (reflect.Value, bool) {
	for _, part := range parts {
		value = indirectValue(value)
		switch value.Kind() {
		case reflect.Struct:
			value = value.FieldByName(part)
		case reflect.Map:
			value = value.MapIndex(reflect.ValueOf(part))
		default:
			return reflect.Value{}, false
		}

		if !value.IsValid() {
			return reflect.Value{}, false
		}
	}

	return value, true
}

func indirectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return reflect.Value{}
		}

		value = value.Elem()
	}

	return value
}

func validateIdentifier(identifier string) error {
	switch identifier {
	case "call", "html", "js", "urlquery":
		return ErrInvalidTemplate
	default:
		return nil
	}
}

func (i *templateInspection) walkBranch(pipe *parse.PipeNode, list, elseList *parse.ListNode, depth int) error {
	if err := i.walk(pipe, depth+1); err != nil {
		return err
	}

	if err := i.walk(list, depth+1); err != nil {
		return err
	}

	return i.walk(elseList, depth+1)
}

func (i *templateInspection) cyclic() bool {
	visiting := make(map[string]bool)
	visited := make(map[string]bool)

	var visit func(string) bool

	visit = func(name string) bool {
		if visiting[name] {
			return true
		}

		if visited[name] {
			return false
		}

		visiting[name] = true
		if slices.ContainsFunc(i.calls[name], visit) {
			return true
		}

		visiting[name] = false
		visited[name] = true

		return false
	}
	for name := range i.calls {
		if visit(name) {
			return true
		}
	}

	return false
}

func renderTemplate(source string, data Data, maxBytes int) (string, error) {
	operations := 0

	functions := templateFunctions(&operations)

	parsed, err := template.New("instruction").Option("missingkey=error").Funcs(functions).Parse(source)
	if err != nil {
		return "", fmt.Errorf("parsing frozen instruction template: %w", ErrInvalidTemplate)
	}

	if err := instrumentRangeBudgets(parsed, functions); err != nil {
		return "", err
	}

	var output bytes.Buffer

	writer := &limitWriter{writer: &output, remaining: maxBytes, operations: maxTemplateOperations}
	if err := parsed.Execute(writer, data); err != nil {
		return "", fmt.Errorf("rendering instruction template: %w", err)
	}

	if operations > maxTemplateOperations {
		return "", ErrRenderBounds
	}

	return output.String(), nil
}

func templateFunctions(operations *int) template.FuncMap {
	count := func() error {
		if operations == nil {
			return nil
		}

		*operations++
		if *operations > maxTemplateOperations {
			return ErrRenderBounds
		}

		return nil
	}

	functions := template.FuncMap{
		"json": func(value any) (string, error) {
			if err := count(); err != nil {
				return "", err
			}

			encoded, err := json.Marshal(value)
			if err != nil {
				return "", fmt.Errorf("encoding template value: %w", err)
			}

			return string(encoded), nil
		},
		"quote": func(value any) (string, error) {
			if err := count(); err != nil {
				return "", err
			}

			switch typed := value.(type) {
			case string:
				return strconv.Quote(typed), nil
			case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, bool:
				encoded, err := json.Marshal(typed)
				return string(encoded), err
			default:
				return "", fmt.Errorf("quoting template value: %w", ErrInvalidTemplate)
			}
		},
	}
	if operations != nil {
		functions["duto_budget_internal"] = func() (string, error) {
			return "", count()
		}
	}

	return functions
}

func instrumentRangeBudgets(parsed *template.Template, functions template.FuncMap) error {
	budgetTemplate, err := template.New("duto-budget").Funcs(functions).Parse(`{{ duto_budget_internal }}`)
	if err != nil {
		return fmt.Errorf("creating template operation budget: %w", err)
	}

	budget := budgetTemplate.Root.Nodes[0]
	for _, named := range parsed.Templates() {
		if named.Tree != nil && named.Root != nil {
			instrumentRangeNodes(named.Root, budget)
		}
	}

	return nil
}

func instrumentRangeNodes(node, budget parse.Node) {
	if node == nil || (reflect.ValueOf(node).Kind() == reflect.Pointer && reflect.ValueOf(node).IsNil()) {
		return
	}

	switch typed := node.(type) {
	case *parse.ListNode:
		for _, child := range typed.Nodes {
			instrumentRangeNodes(child, budget)
		}
	case *parse.RangeNode:
		instrumentRangeNodes(typed.List, budget)
		typed.List.Nodes = append([]parse.Node{budget.Copy()}, typed.List.Nodes...)
		instrumentRangeNodes(typed.ElseList, budget)
	case *parse.IfNode:
		instrumentRangeNodes(typed.List, budget)
		instrumentRangeNodes(typed.ElseList, budget)
	case *parse.WithNode:
		instrumentRangeNodes(typed.List, budget)
		instrumentRangeNodes(typed.ElseList, budget)
	}
}

type limitWriter struct {
	writer     io.Writer
	remaining  int
	operations int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	w.operations--
	if w.operations < 0 || len(p) > w.remaining {
		return 0, ErrRenderBounds
	}

	n, err := w.writer.Write(p)

	w.remaining -= n
	if err != nil {
		return n, fmt.Errorf("writing rendered instruction: %w", err)
	}

	return n, nil
}

func joinPath(parts []string) string {
	return strings.Join(parts, ".")
}
