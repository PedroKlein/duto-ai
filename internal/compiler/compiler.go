package compiler

import (
	"fmt"

	"google.golang.org/adk/v2/workflow"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	"github.com/PedroKlein/duto-ai/internal/tool"
)

// Compile takes a parsed workflow and config, and produces an ADK v2 Workflow.
func Compile(wf *config.Workflow, cfg *config.Config, reg *tool.Registry, eventCtx *prompt.EventContext) (*workflow.Workflow, error) {
	sorted, err := config.TopologicalSort(wf.Steps)
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}

	nodes, err := buildNodes(sorted, cfg, reg, eventCtx)
	if err != nil {
		return nil, fmt.Errorf("building nodes: %w", err)
	}

	edges := buildEdges(sorted, nodes)

	adkWorkflow, err := workflow.New(wf.Name, edges)
	if err != nil {
		return nil, fmt.Errorf("creating workflow: %w", err)
	}

	return adkWorkflow, nil
}

func buildNodes(steps []config.Step, cfg *config.Config, reg *tool.Registry, eventCtx *prompt.EventContext) (map[string]workflow.Node, error) {
	nodes := make(map[string]workflow.Node, len(steps))

	for _, step := range steps {
		node, err := BuildNode(step, cfg, reg, eventCtx)
		if err != nil {
			return nil, fmt.Errorf("building node %q: %w", step.ID, err)
		}

		nodes[step.ID] = node
	}

	return nodes, nil
}

func buildEdges(steps []config.Step, nodes map[string]workflow.Node) []workflow.Edge {
	var edges []workflow.Edge

	for _, step := range steps {
		node := nodes[step.ID]

		if len(step.Needs) == 0 {
			// No dependencies: connect from Start
			edges = append(edges, workflow.Edge{
				From: workflow.Start,
				To:   node,
			})
		} else if len(step.Needs) == 1 {
			// Single dependency: direct edge
			edges = append(edges, workflow.Edge{
				From: nodes[step.Needs[0]],
				To:   node,
			})
		} else {
			// Multiple dependencies: insert a JoinNode
			joinNode := workflow.NewJoinNode(step.ID + "_join")

			for _, dep := range step.Needs {
				edges = append(edges, workflow.Edge{
					From: nodes[dep],
					To:   joinNode,
				})
			}

			edges = append(edges, workflow.Edge{
				From: joinNode,
				To:   node,
			})
		}
	}

	return edges
}
