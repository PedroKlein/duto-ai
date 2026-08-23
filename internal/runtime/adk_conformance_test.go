package runtime_test

import (
	"iter"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestADKConformance_PluginLifecycleRequiresFullIteratorConsumption(t *testing.T) {
	var beforeRun, afterRun bool

	var observed []string

	p, err := plugin.New(plugin.Config{
		Name: "conformance",
		BeforeRunCallback: func(agent.InvocationContext) (*genai.Content, error) {
			beforeRun = true

			return nil, nil
		},
		OnEventCallback: func(_ agent.InvocationContext, event *session.Event) (*session.Event, error) {
			if event != nil && event.Content != nil {
				for _, part := range event.Content.Parts {
					observed = append(observed, part.Text)
				}
			}

			return event, nil
		},
		AfterRunCallback: func(agent.InvocationContext) {
			afterRun = true
		},
	})
	if err != nil {
		t.Fatalf("plugin.New: %v", err)
	}

	root, err := agent.New(agent.Config{
		Name: "plugin-root",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				partial := session.NewEvent(ctx, ctx.InvocationID())
				partial.Author = "plugin-root"
				partial.LLMResponse = model.LLMResponse{
					Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "part"}}},
					Partial: true,
				}

				if !yield(partial, nil) {
					return
				}

				final := session.NewEvent(ctx, ctx.InvocationID())
				final.Author = "plugin-root"
				final.LLMResponse = model.LLMResponse{
					Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{Text: "final"}}},
				}
				yield(final, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	serviceA := session.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:         "conformance",
		Agent:           root,
		SessionService:  serviceA,
		ArtifactService: artifact.InMemoryService(),
		PluginConfig: runner.PluginConfig{
			Plugins: []*plugin.Plugin{p},
		},
		AutoCreateSession: true,
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}

	sequence := r.Run(t.Context(), "user", "session", genai.NewContentFromText("run", genai.RoleUser), agent.RunConfig{})
	if beforeRun || afterRun {
		t.Fatal("plugin callbacks ran before the lazy iterator was consumed")
	}

	var finalNonPartial bool

	for event, err := range sequence {
		if err != nil {
			t.Fatalf("runner.Run: %v", err)
		}

		if event != nil && !event.Partial {
			finalNonPartial = true
		}
	}

	if !beforeRun || !afterRun {
		t.Fatalf("plugin lifecycle = before:%t after:%t, want both true", beforeRun, afterRun)
	}

	if !finalNonPartial {
		t.Fatal("full iterator consumption observed no final non-partial event")
	}

	if diff := cmp.Diff([]string{"part", "final"}, observed); diff != "" {
		t.Fatalf("plugin event order mismatch (-want +got):\n%s", diff)
	}

	serviceB := session.InMemoryService()

	_, err = serviceB.Get(t.Context(), &session.GetRequest{AppName: "conformance", UserID: "user", SessionID: "session"})
	if err == nil {
		t.Fatal("fresh session service unexpectedly contained the preceding run")
	}
}
