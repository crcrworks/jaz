package acp_test

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/log"

	"github.com/wins/jaz/backend/internal/acp"
	"github.com/wins/jaz/backend/internal/agent"
	"github.com/wins/jaz/backend/internal/modelcatalog"
	"github.com/wins/jaz/backend/internal/provider"
	"github.com/wins/jaz/backend/internal/storage"
	jsonstore "github.com/wins/jaz/backend/internal/storage/json"
)

type utilityLocalRunner struct {
	usage   *provider.Usage
	seen    chan acp.LocalUtilityRequest
	text    string
	errText string
	drained chan struct{}
	stall   bool
}

func (r utilityLocalRunner) Run(context.Context, acp.LocalAgentRequest) <-chan agent.StreamEvent {
	out := make(chan agent.StreamEvent)
	close(out)
	return out
}

func (r utilityLocalRunner) RunUtility(_ context.Context, req acp.LocalUtilityRequest) <-chan agent.StreamEvent {
	out := make(chan agent.StreamEvent)
	if r.stall {
		return out
	}
	go func() {
		defer close(out)
		if r.seen != nil {
			r.seen <- req
		}
		if r.errText != "" {
			out <- agent.StreamEvent{Type: agent.StreamError, Error: r.errText}
			out <- agent.StreamEvent{Type: agent.StreamDone, Usage: r.usage}
			if r.drained != nil {
				close(r.drained)
			}
			return
		}
		text := r.text
		if text == "" {
			text = "local utility reply"
		}
		out <- agent.StreamEvent{Type: agent.StreamDelta, Delta: text}
		out <- agent.StreamEvent{Type: agent.StreamDone, Usage: r.usage}
	}()
	return out
}

func newUtilityLocalManager(t *testing.T, runner acp.LocalAgentRunner, cfg acp.AgentConfig) (*jsonstore.Store, *acp.Manager) {
	t.Helper()
	const localAgent = "local_helper"
	store, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := acp.NewManager(store, acp.Config{
		Root:      t.TempDir(),
		Workspace: t.TempDir(),
		Agents: map[string]acp.AgentConfig{
			localAgent: cfg,
		},
	}, log.New(io.Discard))
	manager.RegisterLocalAgent(localAgent, runner)
	return store, manager
}

func TestManagerRunUtilityPromptUsesACPWithoutPersistingSession(t *testing.T) {
	store, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := acp.NewManager(store, acp.Config{
		Root:      t.TempDir(),
		Workspace: t.TempDir(),
		Agents: map[string]acp.AgentConfig{
			"fake": {
				Command: os.Args[0],
				Args:    []string{"-test.run=TestFakeACPAgentProcess"},
				Env:     map[string]string{"JAZ_FAKE_ACP_AGENT": "1"},
			},
		},
	}, log.New(io.Discard))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	text, err := manager.RunUtilityPrompt(ctx, acp.UtilityPromptRequest{
		ACPAgent:  "fake",
		Directory: ".",
		Message:   "return a short title",
		Timeout:   10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello from fake agent" {
		t.Fatalf("text = %q, want fake agent output", text)
	}
	if jobs := manager.List(); len(jobs) != 0 {
		t.Fatalf("jobs = %#v, want no registered job", jobs)
	}
	sessions, err := store.ListSessions(storage.SessionFilter{IncludeChildren: true, IncludeSourced: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %#v, want no stored thread", sessions)
	}
}

func TestManagerRunUtilityPromptUsesLocalRunnerWithoutPersistingSession(t *testing.T) {
	runner := utilityLocalRunner{
		seen: make(chan acp.LocalUtilityRequest, 1),
		text: `{"title":"Local Utility Title"}`,
	}
	store, manager := newUtilityLocalManager(t, runner, acp.AgentConfig{
		Local:           true,
		ModelProvider:   "openrouter",
		Model:           "openai/gpt-test",
		ReasoningEffort: "medium",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text, err := manager.RunUtilityPrompt(ctx, acp.UtilityPromptRequest{
		ACPAgent:  "local_helper",
		Directory: ".",
		Message:   "return a short title",
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"title":"Local Utility Title"}` {
		t.Fatalf("text = %q, want local utility output", text)
	}
	select {
	case req := <-runner.seen:
		if req.Message != "return a short title" ||
			req.Session.ModelProvider != "openrouter" ||
			req.Session.Model != "openai/gpt-test" ||
			req.Session.ReasoningEffort != "medium" ||
			req.Session.RuntimeRef == nil ||
			req.Session.RuntimeRef.Agent != "local_helper" ||
			req.Session.RuntimeRef.Cwd == "" {
			t.Fatalf("local utility request = %#v", req)
		}
	case <-time.After(time.Second):
		t.Fatal("local utility runner was not called")
	}
	if jobs := manager.List(); len(jobs) != 0 {
		t.Fatalf("jobs = %#v, want no registered job", jobs)
	}
	sessions, err := store.ListSessions(storage.SessionFilter{IncludeChildren: true, IncludeSourced: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %#v, want no stored thread", sessions)
	}
}

func TestManagerRunUtilityPromptDrainsLocalErrorStream(t *testing.T) {
	drained := make(chan struct{})
	runner := utilityLocalRunner{errText: "boom", drained: drained}
	_, manager := newUtilityLocalManager(t, runner, acp.AgentConfig{Local: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := manager.RunUtilityPrompt(ctx, acp.UtilityPromptRequest{
		ACPAgent:  "local_helper",
		Directory: ".",
		Message:   "return a short title",
	})
	if err == nil || err.Error() != "local utility prompt failed: boom" {
		t.Fatalf("err = %v, want local utility error", err)
	}
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("local utility stream was not drained after error")
	}
}

func TestManagerRunUtilityPromptTimesOutStalledLocalStream(t *testing.T) {
	_, manager := newUtilityLocalManager(t, utilityLocalRunner{stall: true}, acp.AgentConfig{Local: true})

	_, err := manager.RunUtilityPrompt(context.Background(), acp.UtilityPromptRequest{
		ACPAgent:  "local_helper",
		Directory: ".",
		Message:   "return a short title",
		Timeout:   20 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestUtilityUsageIsRecordedOnSuccessAndFailure(t *testing.T) {
	for _, failure := range []string{"", "failed after work"} {
		t.Run(failure, func(t *testing.T) {
			store, manager := newUtilityLocalManager(t, utilityLocalRunner{
				errText: failure,
				usage:   &provider.Usage{InputTokens: 120, CachedInputTokens: 100, OutputTokens: 10, TotalTokens: 130},
			}, acp.AgentConfig{Local: true})
			session, err := store.CreateSession(storage.CreateSession{Slug: "title-usage"})
			if err != nil {
				t.Fatal(err)
			}
			session.Usage.ContextTokens = 999
			if err := store.SaveSession(session); err != nil {
				t.Fatal(err)
			}
			_, err = manager.RunUtilityPrompt(context.Background(), acp.UtilityPromptRequest{
				SessionID: session.ID,
				ACPAgent:  "local_helper",
				Directory: ".",
				Message:   "title",
			})
			if (err != nil) != (failure != "") {
				t.Fatalf("error = %v", err)
			}
			loaded, err := store.LoadSession(session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Usage.InputTokens != 120 || loaded.Usage.CachedInputTokens != 100 || loaded.Usage.OutputTokens != 10 || loaded.Usage.ContextTokens != 999 {
				t.Fatalf("title usage = %#v", loaded.Usage)
			}
		})
	}
}

// A utility prompt asks for no reasoning at all so a title never inherits the
// thread's high effort. It must stay clear of the agent's levels: on OpenCode a
// "none" level exists, and the model's provider entry need not offer it.
func TestManagerRunUtilityPromptAsksForNoReasoning(t *testing.T) {
	store, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range []string{"openai/gpt-5.5", "unknown-model"} {
		t.Run(model, func(t *testing.T) {
			manager := acp.NewManager(store, acp.Config{
				Root:         t.TempDir(),
				Workspace:    t.TempDir(),
				ModelCatalog: warmedManagerModelCatalog(t),
				Agents: map[string]acp.AgentConfig{
					acp.AgentOpenCode: {
						Command:         os.Args[0],
						Args:            []string{"-test.run=TestFakeACPAgentProcess"},
						ProviderMode:    acp.AgentProviderModeAgentDefaults,
						ModelProvider:   provider.ProviderOpenRouter,
						Model:           model,
						ReasoningEffort: "xhigh",
						Env:             map[string]string{"JAZ_FAKE_ACP_AGENT": "1"},
					},
				},
			}, log.New(io.Discard))
			t.Cleanup(manager.Close)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			text, err := manager.RunUtilityPrompt(ctx, acp.UtilityPromptRequest{
				ACPAgent:          acp.AgentOpenCode,
				ModelProvider:     provider.ProviderOpenRouter,
				Model:             model,
				Message:           "return a short title",
				NoReasoningEffort: true,
				Timeout:           10 * time.Second,
			})
			if err != nil {
				t.Fatalf("title prompt failed: %v", err)
			}
			if text == "" {
				t.Fatal("title prompt returned nothing")
			}
			sessions, err := store.ListSessions(storage.SessionFilter{IncludeChildren: true, IncludeSourced: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(sessions) != 0 {
				t.Fatalf("sessions = %#v, want no stored thread", sessions)
			}
		})
	}
}

// A cold provider catalog must not fail a title either: the prompt asks for no
// effort, so there is nothing to validate against model metadata.
func TestManagerRunUtilityPromptSurvivesColdCatalog(t *testing.T) {
	store, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := acp.NewManager(store, acp.Config{
		Root:      t.TempDir(),
		Workspace: t.TempDir(),
		ModelCatalog: modelcatalog.NewService(provider.StaticSource(map[string]provider.ModelProviderConfig{
			provider.ProviderOpenRouter: {BaseURL: "http://127.0.0.1:1"},
		})),
		Agents: map[string]acp.AgentConfig{
			acp.AgentOpenCode: {
				Command:         os.Args[0],
				Args:            []string{"-test.run=TestFakeACPAgentProcess"},
				ProviderMode:    acp.AgentProviderModeAgentDefaults,
				ModelProvider:   provider.ProviderOpenRouter,
				Model:           "z-ai/glm-5.2",
				ReasoningEffort: "xhigh",
				Env:             map[string]string{"JAZ_FAKE_ACP_AGENT": "1"},
			},
		},
	}, log.New(io.Discard))
	t.Cleanup(manager.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := manager.RunUtilityPrompt(ctx, acp.UtilityPromptRequest{
		ACPAgent:          acp.AgentOpenCode,
		ModelProvider:     provider.ProviderOpenRouter,
		Model:             "z-ai/glm-5.2",
		Message:           "return a short title",
		NoReasoningEffort: true,
		Timeout:           10 * time.Second,
	}); err != nil {
		t.Fatalf("title prompt failed with a cold catalog: %v", err)
	}
}
