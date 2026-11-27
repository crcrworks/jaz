package acp

import (
	"errors"
	"strings"
	"testing"

	"github.com/wins/jaz/backend/internal/modelcatalog"
	"github.com/wins/jaz/backend/internal/provider"
)

func TestModelCapabilitiesAddsCodexUltraWithoutInventingMinimal(t *testing.T) {
	model := modelcatalog.Model{
		Value:        provider.OpenAIModelGPT6Sol,
		OpenRouterID: "openai/gpt-6-sol",
		Reasoning: modelcatalog.Reasoning{
			Status:  modelcatalog.ReasoningReady,
			Efforts: []string{"low", "medium", "high", "xhigh", "max"},
		},
	}
	catalog := capabilityCatalog{
		agents:    map[string][]modelcatalog.Model{AgentCodex: {model}},
		providers: map[string][]modelcatalog.Model{provider.ProviderOpenAI: {model}},
	}
	capabilities := ModelCapabilities{Catalog: catalog}
	models := capabilities.AgentModels(AgentCodex)
	if got := strings.Join(models[0].Reasoning.Efforts, ","); got != "low,medium,high,xhigh,max,ultra" {
		t.Fatalf("reasoning efforts = %q", got)
	}
	if models[0].Reasoning.Scope != ReasoningScopeProvider {
		t.Fatalf("reasoning scope = %q", models[0].Reasoning.Scope)
	}
	if err := capabilities.ValidateReasoningEffort(AgentCodex, provider.ProviderOpenAI, provider.OpenAIModelGPT6Sol, "minimal"); err == nil {
		t.Fatal("expected minimal to be rejected")
	}
	if err := capabilities.ValidateReasoningEffort(AgentCodex, provider.ProviderOpenAI, provider.OpenAIModelGPT6Sol, "ultra"); err != nil {
		t.Fatal(err)
	}
}

func TestCodexUltraModelsUseExplicitAllowlist(t *testing.T) {
	for _, test := range []struct {
		model string
		want  bool
	}{
		{provider.OpenAIModelGPT6Astra, true},
		{provider.ProviderOpenAI + "/" + provider.OpenAIModelGPT6Astra, true},
		{provider.OpenAIModelGPT61Sol, true},
		{provider.ProviderOpenAI + "/" + provider.OpenAIModelGPT61Sol, true},
		{provider.OpenAIModelGPT6Sol, true},
		{provider.ProviderOpenAI + "/" + provider.OpenAIModelGPT6Sol, true},
		{"gpt-6-luna", false},
		{provider.ProviderOpenAI + "/gpt-6-luna", false},
	} {
		if got := isCodexUltraModel(modelcatalog.Model{Value: test.model}); got != test.want {
			t.Fatalf("isCodexUltraModel(%q) = %v, want %v", test.model, got, test.want)
		}
	}
	luna := modelcatalog.Model{
		Value: "gpt-6-luna",
		Reasoning: modelcatalog.Reasoning{
			Status:  modelcatalog.ReasoningReady,
			Efforts: []string{"max", "ultra"},
		},
	}
	models := (ModelCapabilities{Catalog: capabilityCatalog{agents: map[string][]modelcatalog.Model{AgentCodex: {luna}}}}).AgentModels(AgentCodex)
	if got := strings.Join(models[0].Reasoning.Efforts, ","); got != "max" {
		t.Fatalf("Luna reasoning efforts = %q", got)
	}
	spark := modelcatalog.Model{
		Value:     "gpt-5.3-codex-spark",
		Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningUnavailable},
	}
	models = (ModelCapabilities{Catalog: capabilityCatalog{agents: map[string][]modelcatalog.Model{AgentCodex: {spark}}}}).AgentModels(AgentCodex)
	if containsString(models[0].Reasoning.Efforts, "ultra") {
		t.Fatalf("non-allowlisted fallback efforts = %v", models[0].Reasoning.Efforts)
	}
}

func TestModelCatalogRoutingByAgentAndProvider(t *testing.T) {
	for _, test := range []struct {
		agent     string
		provider  string
		agentOwns bool
		curated   bool
	}{
		{AgentCodex, "", true, true},
		{AgentClaude, "", true, true},
		{AgentCodex, provider.ProviderOpenAI, true, true},
		{AgentCodex, CodexProviderOpenAIAPIKey, true, true},
		{AgentCodex, provider.ProviderOpenRouter, false, true},
		{AgentCodex, provider.ProviderOllama, false, false},
		{AgentOpenCode, provider.ProviderOpenRouter, false, true},
		{AgentOpenCode, provider.ProviderOpenAI, false, false},
		{AgentClaude, provider.ProviderOpenAI, false, false},
	} {
		name := test.agent + "/" + test.provider
		if got := agentOwnsModelMetadata(test.agent, test.provider); got != test.agentOwns {
			t.Errorf("%s agentOwnsModelMetadata = %v, want %v", name, got, test.agentOwns)
		}
		if got := usesCuratedModels(test.agent, test.provider); got != test.curated {
			t.Errorf("%s usesCuratedModels = %v, want %v", name, got, test.curated)
		}
	}
}

func TestModelCapabilitiesUsesCodexHarnessForOpenAIModelsWithoutProviderMetadata(t *testing.T) {
	capabilities := ModelCapabilities{Catalog: modelcatalog.NewService(nil)}
	if err := capabilities.ValidateReasoningEffort(AgentCodex, provider.ProviderOpenAI, "gpt-5.3-codex-spark", "xhigh"); err != nil {
		t.Fatal(err)
	}
}

func TestModelCapabilitiesPopulatesNativeAgentReasoningCapabilities(t *testing.T) {
	capabilities := ModelCapabilities{Catalog: modelcatalog.NewService(nil)}
	models := capabilities.AgentModels(AgentCodex)
	if len(models) == 0 || models[0].Reasoning.Status != modelcatalog.ReasoningReady {
		t.Fatalf("models = %#v", models)
	}
	if !containsString(models[0].Reasoning.Efforts, "ultra") {
		t.Fatalf("%s efforts = %#v, want ultra", models[0].Value, models[0].Reasoning.Efforts)
	}
	if err := capabilities.ValidateReasoningEffort(AgentCodex, "", provider.OpenAIModelGPT6Sol, "ultra"); err != nil {
		t.Fatal(err)
	}
}

func TestModelCapabilitiesLeavesProviderBackedModelsUnknownUntilCatalogLoads(t *testing.T) {
	capabilities := ModelCapabilities{Catalog: modelcatalog.NewService(nil)}
	_, err := capabilities.ProviderModels(AgentCodex, provider.ProviderOpenRouter)
	if !errors.Is(err, modelcatalog.ErrCatalogUnavailable) {
		t.Fatalf("err = %v, want ErrCatalogUnavailable", err)
	}
}

func TestModelCapabilitiesValidatesSelectedProviderBeforeAgentCatalog(t *testing.T) {
	agentModel := modelcatalog.Model{Value: "shared", Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningReady, Efforts: []string{"minimal"}}}
	providerModel := modelcatalog.Model{Value: "shared", Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningReady, Efforts: []string{"high"}}}
	capabilities := ModelCapabilities{Catalog: capabilityCatalog{
		agents:    map[string][]modelcatalog.Model{AgentCodex: {agentModel}},
		providers: map[string][]modelcatalog.Model{"custom": {providerModel}},
	}}
	if err := capabilities.ValidateReasoningEffort(AgentCodex, "custom", "shared", "minimal"); err == nil {
		t.Fatal("expected selected provider capabilities to reject minimal")
	}
}

func TestModelCapabilitiesPreservesAutomaticProviderReasoning(t *testing.T) {
	models, err := (ModelCapabilities{Catalog: capabilityCatalog{providers: map[string][]modelcatalog.Model{
		provider.ProviderOllama: {{
			Value:     "qwen3.6:latest",
			Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningReady, Automatic: true},
		}},
	}}}).ProviderModels(AgentCodex, provider.ProviderOllama)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || !models[0].Reasoning.Automatic || models[0].Reasoning.Scope != ReasoningScopeProvider {
		t.Fatalf("models = %#v", models)
	}
}

func TestModelCapabilitiesAddsCuratedAliasesToProviderModels(t *testing.T) {
	catalog := capabilityCatalog{
		agents: map[string][]modelcatalog.Model{AgentCodex: {{
			Value:        provider.OpenAIModelGPT6Sol,
			Label:        "GPT-6 Sol",
			OpenRouterID: "openai/gpt-6-sol",
		}}},
		providers: map[string][]modelcatalog.Model{provider.ProviderOpenRouter: {{
			Value: "openai/gpt-6-sol",
			Reasoning: modelcatalog.Reasoning{
				Status:  modelcatalog.ReasoningReady,
				Efforts: []string{"high"},
			},
		}}},
	}
	models, err := (ModelCapabilities{Catalog: catalog}).ProviderModels(AgentCodex, provider.ProviderOpenRouter)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || strings.Join(models[0].Aliases, ",") != "gpt-6-sol,GPT-6 Sol" {
		t.Fatalf("models = %#v", models)
	}
	if err := (ModelCapabilities{Catalog: catalog}).ValidateReasoningEffort(AgentCodex, provider.ProviderOpenRouter, provider.OpenAIModelGPT6Sol, "high"); err != nil {
		t.Fatal(err)
	}
}

func TestModelCapabilitiesAddsClaudeUltracodeOnlyToXhighModels(t *testing.T) {
	catalog := capabilityCatalog{agents: map[string][]modelcatalog.Model{
		AgentClaude: {
			{Value: "opus", Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningReady, Efforts: []string{"low", "xhigh", "max"}}},
			{Value: "sonnet", Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningReady, Efforts: []string{"low", "high", "max"}}},
		},
	}}
	models := (ModelCapabilities{Catalog: catalog}).AgentModels(AgentClaude)
	if got := strings.Join(models[0].Reasoning.Efforts, ","); got != "low,xhigh,max,ultracode" {
		t.Fatalf("opus efforts = %q", got)
	}
	if got := strings.Join(models[1].Reasoning.Efforts, ","); got != "low,high,max" {
		t.Fatalf("sonnet efforts = %q", got)
	}
}

type capabilityCatalog struct {
	agents    map[string][]modelcatalog.Model
	providers map[string][]modelcatalog.Model
}

func (c capabilityCatalog) AgentModels(agent string) []modelcatalog.Model {
	return append([]modelcatalog.Model(nil), c.agents[agent]...)
}

func (c capabilityCatalog) ProviderModels(providerID string) ([]modelcatalog.Model, error) {
	return append([]modelcatalog.Model(nil), c.providers[providerID]...), nil
}

// A provider that lists a model without describing its reasoning leaves the
// effort to the agent's own session contract instead of failing the start.
func TestModelCapabilitiesDefersUnknownReasoningToTheAgent(t *testing.T) {
	catalog := capabilityCatalog{providers: map[string][]modelcatalog.Model{
		provider.ProviderOpenAI: {{Value: "deepseek-v4.1-flash", Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningUnknown}}},
	}}
	capabilities := ModelCapabilities{Catalog: catalog}
	if err := capabilities.ValidateReasoningEffort(AgentOpenCode, provider.ProviderOpenAI, "deepseek-v4.1-flash", "xhigh"); err != nil {
		t.Fatal(err)
	}
	if effort := capabilities.sessionReasoningEffort(AgentOpenCode, provider.ProviderOpenAI, "deepseek-v4.1-flash", "xhigh"); effort != "xhigh" {
		t.Fatalf("unknown reasoning session effort = %q, want xhigh kept for the agent to judge", effort)
	}
}

// A model the provider lists as advertising no reasoning still refuses an
// effort, so a known non-reasoning model is not silently started on one.
func TestModelCapabilitiesRefusesEffortOnModelsWithoutReasoning(t *testing.T) {
	catalog := capabilityCatalog{providers: map[string][]modelcatalog.Model{
		provider.ProviderOpenAI: {{Value: "plain", Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningUnavailable}}},
	}}
	err := (ModelCapabilities{Catalog: catalog}).ValidateReasoningEffort(AgentOpenCode, provider.ProviderOpenAI, "plain", "xhigh")
	if !errors.Is(err, ErrReasoningCapabilitiesUnavailable) {
		t.Fatalf("err = %v, want ErrReasoningCapabilitiesUnavailable", err)
	}
}

// Moving to a model with fewer thinking levels drops the saved effort for the
// session, the way the picker does, instead of refusing to start.
func TestModelCapabilitiesDropsAnEffortTheNewModelLacks(t *testing.T) {
	capabilities := ModelCapabilities{Catalog: capabilityCatalog{providers: map[string][]modelcatalog.Model{
		provider.ProviderOpenAI: {{Value: "model", Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningReady, Efforts: []string{"low", "medium"}}}},
	}}}
	if err := capabilities.ValidateReasoningEffort(AgentOpenCode, provider.ProviderOpenAI, "model", "xhigh"); err == nil {
		t.Fatal("expected the provider's advertised levels to reject xhigh")
	}
	if effort := capabilities.sessionReasoningEffort(AgentOpenCode, provider.ProviderOpenAI, "model", "xhigh"); effort != "" {
		t.Fatalf("session effort = %q, want none for the model's own default", effort)
	}
	if effort := capabilities.sessionReasoningEffort(AgentClaude, provider.ProviderOpenAI, "model", "high"); effort != "high" {
		t.Fatalf("claude effort = %q, want it validated rather than dropped", effort)
	}
}

// "none" is thinking off for an agent that names it among its levels, which no
// model has to list; the UI keeps it and so does the session.
func TestModelCapabilitiesKeepsNoneForAgentsThatNameIt(t *testing.T) {
	capabilities := ModelCapabilities{Catalog: capabilityCatalog{providers: map[string][]modelcatalog.Model{
		provider.ProviderOpenAI: {{Value: "model", Reasoning: modelcatalog.Reasoning{Status: modelcatalog.ReasoningReady, Efforts: []string{"low", "medium", "high"}}}},
	}}}
	if err := capabilities.ValidateReasoningEffort(AgentOpenCode, provider.ProviderOpenAI, "model", "none"); err != nil {
		t.Fatal(err)
	}
	if effort := capabilities.sessionReasoningEffort(AgentOpenCode, provider.ProviderOpenAI, "model", "none"); effort != "none" {
		t.Fatalf("session effort = %q, want none kept so the session turns thinking off", effort)
	}
	// An agent without a none level means "clear the effort" by it, which
	// normalization empties before an effort reaches validation.
	if got, err := NormalizeAgentReasoningEffort(AgentClaude, "none"); err != nil || got != "" {
		t.Fatalf("claude none = %q, %v; want the effort cleared", got, err)
	}
}
