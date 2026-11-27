package pirpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	// AgentName is what Jaz records as the thread's agent for pi.
	AgentName = "pi"
	// DisplayName is what Settings shows for it.
	DisplayName = "Pi"
	// Version is the ACP implementation version the bridge reports.
	Version = "1"
)

const (
	configModel        = "model"
	configThoughtLevel = "thought_level"
	// compactPrompt is how Jaz asks every agent to compact. pi has no such
	// slash command over RPC: its own `compact` command is the compaction.
	compactPrompt = "/compact"
)

// piEvent maps one pi session event onto the ACP update the manager renders.
// Everything else pi reports is its own business.
func piEvent(kind string, raw json.RawMessage) any {
	switch kind {
	case "message_update":
		return messageUpdate(raw)
	case "tool_execution_start", "tool_execution_update", "tool_execution_end":
		return toolUpdate(kind, raw)
	}
	return nil
}

func messageUpdate(raw json.RawMessage) any {
	var event struct {
		AssistantMessageEvent struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		} `json:"assistantMessageEvent"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return nil
	}
	inner := event.AssistantMessageEvent
	if inner.Delta == "" {
		return nil
	}
	switch inner.Type {
	case "text_delta":
		return agentChunk("agent_message_chunk", inner.Delta)
	case "thinking_delta":
		return agentChunk("agent_thought_chunk", inner.Delta)
	}
	return nil
}

func agentChunk(kind, text string) map[string]any {
	return map[string]any{
		"sessionUpdate": kind,
		"content":       map[string]any{"type": "text", "text": text},
	}
}

func toolUpdate(kind string, raw json.RawMessage) any {
	var event struct {
		ToolCallID    string          `json:"toolCallId"`
		ToolName      string          `json:"toolName"`
		Args          json.RawMessage `json:"args"`
		Result        json.RawMessage `json:"result"`
		PartialResult json.RawMessage `json:"partialResult"`
		IsError       bool            `json:"isError"`
	}
	if json.Unmarshal(raw, &event) != nil || event.ToolCallID == "" {
		return nil
	}
	update := map[string]any{
		"toolCallId":    event.ToolCallID,
		"name":          event.ToolName,
		"title":         event.ToolName,
		"status":        "in_progress",
		"sessionUpdate": "tool_call_update",
	}
	if kind == "tool_execution_start" {
		update["sessionUpdate"] = "tool_call"
	}
	if len(event.Args) > 0 {
		update["rawInput"] = event.Args
	}
	switch kind {
	case "tool_execution_update":
		if text := resultText(event.PartialResult); text != "" {
			update["content"] = toolText(text)
		}
	case "tool_execution_end":
		update["status"] = "completed"
		if event.IsError {
			update["status"] = "failed"
		}
		if text := resultText(event.Result); text != "" {
			update["content"] = toolText(text)
		}
	}
	return update
}

func toolText(text string) []map[string]any {
	return []map[string]any{{"type": "content", "content": map[string]any{"type": "text", "text": text}}}
}

// resultText is the text a pi tool result carries, joined as the model sees it.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return ""
	}
	texts := make([]string, 0, len(result.Content))
	for _, block := range result.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// piModel is the part of pi's model object Jaz reads: pi owns these values and
// Jaz reports them as they are.
type piModel struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Provider       string         `json:"provider"`
	ContextWindow  int            `json:"contextWindow"`
	MaxTokens      int            `json:"maxTokens"`
	Reasoning      bool           `json:"reasoning"`
	Input          []string       `json:"input"`
	ThinkingLevels map[string]any `json:"thinkingLevelMap"`
}

// Qualified is the id a session selects this model by: pi runs models of many
// providers, and only "provider/id" names one of them.
func (m piModel) Qualified() string {
	if m.Provider == "" {
		return m.ID
	}
	return m.Provider + "/" + m.ID
}

var thinkingOrder = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// Levels is the thinking levels pi says this model runs, in pi's own order.
func (m piModel) Levels() []string {
	if len(m.ThinkingLevels) == 0 {
		if m.Reasoning {
			return nil
		}
		return []string{"off"}
	}
	out := make([]string, 0, len(thinkingOrder))
	for _, level := range thinkingOrder {
		if value, ok := m.ThinkingLevels[level]; ok && value != nil {
			out = append(out, level)
		}
	}
	return out
}

type piState struct {
	Model         *piModel `json:"model"`
	ThinkingLevel string   `json:"thinkingLevel"`
}

// modelCatalog reads pi's own model list, which is what Jaz reports for a pi
// thread: pi's values, with nothing guessed for what pi leaves out.
type modelCatalog struct {
	Models []piModel `json:"models"`
}

// optionCatalog is what a session's config options describe.
type optionCatalog struct {
	model  string
	effort string
	models []piModel
	levels []string
}

// describe reads what pi offers and what it is running, so the ACP side can
// offer exactly that.
func (s *bridgeSession) describe(ctx context.Context) optionCatalog {
	catalog := optionCatalog{model: s.model, effort: s.effort}
	var state piState
	if err := s.client.Call(ctx, "get_state", nil, &state); err == nil {
		if state.Model != nil {
			catalog.model = state.Model.Qualified()
		}
		catalog.effort = firstNonEmpty(state.ThinkingLevel, catalog.effort)
	}
	var models modelCatalog
	if err := s.client.Call(ctx, "get_available_models", nil, &models); err == nil {
		catalog.models = models.Models
	}
	var levels struct {
		Levels []string `json:"levels"`
	}
	if err := s.client.Call(ctx, "get_available_thinking_levels", nil, &levels); err == nil {
		catalog.levels = levels.Levels
	}
	return catalog
}

// DiscoverModels reads pi's model list from a scratch process, the one place
// Jaz asks pi directly rather than through a session.
func DiscoverModels(ctx context.Context, cfg Config) (ModelCatalog, error) {
	client, err := Start(ctx, Launch{Command: cfg.Command, Env: cfg.Env, Args: []string{"--mode", "rpc", "--no-approve"}})
	if err != nil {
		return ModelCatalog{}, err
	}
	defer client.Close()
	var models modelCatalog
	if err := client.Call(ctx, "get_available_models", nil, &models); err != nil {
		return ModelCatalog{}, err
	}
	out := ModelCatalog{Models: make([]Model, 0, len(models.Models))}
	for _, model := range models.Models {
		out.Models = append(out.Models, Model{
			ID:            model.Qualified(),
			Label:         firstNonEmpty(model.Name, model.ID),
			Provider:      model.Provider,
			ContextLength: model.ContextWindow,
			MaxTokens:     model.MaxTokens,
			Reasoning:     model.Reasoning,
			Inputs:        model.Input,
			Levels:        model.Levels(),
		})
	}
	return out, nil
}

// ModelCatalog is pi's model metadata as Jaz reports it.
type ModelCatalog struct {
	Models []Model `json:"models"`
}

type Model struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	Provider      string   `json:"provider"`
	ContextLength int      `json:"context_length,omitempty"`
	MaxTokens     int      `json:"max_tokens,omitempty"`
	Reasoning     bool     `json:"reasoning"`
	Inputs        []string `json:"input,omitempty"`
	Levels        []string `json:"thinking_levels,omitempty"`
}

// ErrNoExecutable marks pi missing from the server, a setup problem rather
// than a thread failure.
var ErrNoExecutable = errors.New("pi executable not found")

// CheckInstalled reports a missing pi before a thread depends on it.
func CheckInstalled(command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("%w: pi is not configured", ErrNoExecutable)
	}
	return nil
}

// newSessionID names the pi session file a fresh thread runs on. Pi reopens a
// session from its id, so the ACP session id Jaz stores and the file pi writes
// are one identity.
func newSessionID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return "pi-" + hex.EncodeToString(raw[:])
}

func isCompactPrompt(message string) bool {
	return strings.TrimSpace(message) == compactPrompt
}
