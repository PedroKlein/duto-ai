// Package compiler transforms workflow configuration into an ADK v2 workflow agent.
package compiler

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/workflow"

	"github.com/PedroKlein/duto-ai/internal/config"
	"github.com/PedroKlein/duto-ai/internal/prompt"
	"github.com/PedroKlein/duto-ai/internal/tool"
)

// Compile transforms a parsed workflow and config into a ready-to-run ADK workflow agent.
// The returned agent.Agent can be passed directly to runner.New as the root agent.
func Compile(wf *config.Workflow, cfg *config.Config, reg *tool.Registry, llm model.LLM, eventCtx *prompt.EventContext) (agent.Agent, error) {
	sorted, err := config.TopologicalSort(wf.Steps)
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}

	nodes, agents, err := buildNodes(sorted, cfg, reg, llm, eventCtx)
	if err != nil {
		return nil, err
	}

	edges := buildEdges(sorted, nodes)

	root, err := workflowagent.New(workflowagent.Config{
		Name:        wf.Name,
		Description: fmt.Sprintf("Workflow %q with %d steps", wf.Name, len(sorted)),
		Edges:       edges,
		SubAgents:   agents,
	})
	if err != nil {
		return nil, fmt.Errorf("creating workflow agent: %w", err)
	}

	return root, nil
}

func buildNodes(steps []config.Step, cfg *config.Config, reg *tool.Registry, llm model.LLM, eventCtx *prompt.EventContext) (map[string]workflow.Node, []agent.Agent, error) {
	nodes := make(map[string]workflow.Node, len(steps))
	agents := make([]agent.Agent, 0, len(steps))

	for _, step := range steps {
		node, a, err := buildNode(step, cfg, reg, llm, eventCtx)
		if err != nil {
			return nil, nil, fmt.Errorf("building node %q: %w", step.ID, err)
		}

		nodes[step.ID] = node

		agents = append(agents, a)
	}

	return nodes, agents, nil
}

func buildEdges(steps []config.Step, nodes map[string]workflow.Node) []workflow.Edge {
	eb := workflow.NewEdgeBuilder()

	for _, step := range steps {
		node := nodes[step.ID]

		switch {
		case len(step.Needs) == 0:
			eb.Add(workflow.Start, node)

		case len(step.Needs) == 1:
			eb.Add(nodes[step.Needs[0]], node)

		default:
			joinNode := workflow.NewJoinNode(step.ID + "_join")
			predecessors := make([]workflow.Node, 0, len(step.Needs))

			for _, dep := range step.Needs {
				predecessors = append(predecessors, nodes[dep])
			}

			eb.AddFanIn(joinNode, predecessors...)
			eb.Add(joinNode, node)
		}
	}

	return eb.Build()
}
