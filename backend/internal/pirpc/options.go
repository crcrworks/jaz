package pirpc

import (
	"context"
	"strings"

	acpschema "github.com/gluonfield/acp-transport/acp"
)

// configOptions describes the model and thinking level pi reports, shaped as
// ACP's select options so the ACP side offers exactly what pi offers. The typed
// ACP struct holds only the union's common fields, so the select half is built
// here.
func (s *bridgeSession) configOptions(ctx context.Context) []map[string]any {
	catalog := s.describe(ctx)
	models := make([]map[string]any, 0, len(catalog.models))
	for _, candidate := range catalog.models {
		models = append(models, map[string]any{
			"value":       candidate.Qualified(),
			"name":        firstNonEmpty(candidate.Name, candidate.ID),
			"description": candidate.Provider,
		})
	}
	levels := make([]map[string]any, 0, len(catalog.levels))
	for _, level := range catalog.levels {
		levels = append(levels, map[string]any{"value": level, "name": labelled(level)})
	}
	return []map[string]any{
		{
			"id": configModel, "name": "Model", "category": "model", "type": "select",
			"currentValue": catalog.model, "options": models,
		},
		{
			"id": configThoughtLevel, "name": "Thinking", "category": "thought_level", "type": "select",
			"currentValue": catalog.effort, "options": levels,
		},
	}
}

func labelled(level string) string {
	if level == "" {
		return level
	}
	return strings.ToUpper(level[:1]) + level[1:]
}

// set applies the ACP side's choice to pi, which owns the real switch, and
// answers with the options pi now reports.
func (s *bridgeSession) set(ctx context.Context, configID, value string) ([]map[string]any, error) {
	switch configID {
	case configModel:
		provider, id := splitModel(value)
		if err := s.client.Call(ctx, "set_model", map[string]any{"provider": provider, "modelId": id}, nil); err != nil {
			return nil, err
		}
		s.model = value
	case configThoughtLevel:
		if err := s.client.Call(ctx, "set_thinking_level", map[string]any{"level": value}, nil); err != nil {
			return nil, err
		}
		s.effort = value
	default:
		return nil, &CommandError{Command: configID, Message: "pi has no config option " + configID}
	}
	return s.configOptions(ctx), nil
}

// promptText is the text of an ACP prompt; attachments are resource links,
// which pi takes as file references in the text.
func promptText(blocks []acpschema.ContentBlock) (string, error) {
	parts := make([]string, 0, len(blocks))
	for _, raw := range blocks {
		block, err := acpschema.DecodeContentBlock(raw)
		if err != nil {
			return "", err
		}
		switch value := block.(type) {
		case acpschema.TextContentBlock:
			if strings.TrimSpace(value.Text) != "" {
				parts = append(parts, value.Text)
			}
		case acpschema.ResourceLinkContentBlock:
			parts = append(parts, "@"+value.URI)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func splitModel(value string) (provider, id string) {
	provider, id, _ = strings.Cut(strings.TrimSpace(value), "/")
	if id == "" {
		return "", provider
	}
	return provider, id
}

// sessionMetaPrompt is the prompt the manager passes at session/new.
func sessionMetaPrompt(meta map[string]any) string {
	if text, ok := meta["systemPrompt"].(string); ok {
		return text
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
