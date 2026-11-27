package acp

import (
	"context"
	"encoding/json"
	"strings"

	mcpconfig "github.com/wins/jaz/backend/internal/mcpconfig"
	"github.com/wins/jaz/backend/internal/modelcatalog"
	"github.com/wins/jaz/backend/internal/mcpsession"
	"github.com/wins/jaz/backend/internal/pirpc"
)

// piBridgeConfig is what the in-process bridge needs to run a pi process for
// this thread: the isolated profile, Jaz's own prompt at session creation, and
// Jaz's MCP servers, which pi takes through its own client.
func (m *Manager) piBridgeConfig(cfg AgentConfig) pirpc.Config {
	root := m.cfg.Root
	env := m.probeEnv(AgentPi, cfg)
	return pirpc.Config{
		Command: piCommand(cfg),
		Env:     env,
		Profile: pirpc.NewProfile(root),
		Prompt: func(ctx context.Context, cwd string) (string, error) {
			return m.systemPrompt(ctx, cwd, "", MCPServerPolicyAll, nil)
		},
		Servers: func(ctx context.Context) ([]pirpc.MCPServer, error) {
			return piMCPServers(ctx, m.cfg.MCPStore)
		},
	}
}

// piMCPServers is Jaz's server list as pi's client needs it, with the calling
// thread's session header resolved so its tools work in that thread.
func piMCPServers(ctx context.Context, store mcpconfig.ServerReader) ([]pirpc.MCPServer, error) {
	servers, err := enabledHTTPMCPServers(ctx, store, MCPServerPolicyAll)
	if err != nil || servers == nil {
		return nil, err
	}
	out := make([]pirpc.MCPServer, 0, len(servers))
	for _, raw := range servers {
		var server httpMCPServer
		if err := json.Unmarshal(raw, &server); err != nil {
			continue
		}
		headers := map[string]string{}
		for _, header := range server.Headers {
			if strings.EqualFold(header.Name, mcpsession.HeaderName) && strings.TrimSpace(header.Value) == mcpsession.HeaderPlaceholder {
				continue
			}
			headers[header.Name] = header.Value
		}
		out = append(out, pirpc.MCPServer{Name: server.Name, URL: server.URL, Headers: headers})
	}
	return out, nil
}

// piModels reports pi's own model metadata for a Settings thread's picker.
func (m *Manager) piModels(ctx context.Context, cfg AgentConfig) (pirpc.ModelCatalog, error) {
	return pirpc.DiscoverModels(ctx, pirpc.Config{
		Command: piCommand(cfg),
		Env:     m.probeEnv(AgentPi, cfg),
		Profile: pirpc.NewProfile(m.cfg.Root),
	})
}

// piModelCatalog reports pi's own model list, with pi's context limits and
// thinking levels. A model pi states nothing about keeps an empty value.
func (m *Manager) piModelCatalog(ctx context.Context, cfg AgentConfig) (AgentModelCatalog, error) {
	if piCommand(cfg) == "" {
		return AgentModelCatalog{}, pirpc.ErrNoExecutable
	}
	catalog, err := m.piModels(ctx, cfg)
	if err != nil {
		return AgentModelCatalog{}, err
	}
	result := AgentModelCatalog{Models: make([]AgentModel, 0, len(catalog.Models))}
	for _, model := range catalog.Models {
		result.Models = append(result.Models, AgentModel{
			Value:           model.ID,
			Label:           model.Label,
			Description:     model.Provider,
			ContextLength:   model.ContextLength,
			InputModalities: model.Inputs,
			Reasoning: ReasoningCapabilities{
				Status:  modelcatalog.ReasoningUnavailable,
				Scope:   ReasoningScopeAgent,
				Efforts: model.Levels,
			},
		})
	}
	return result, nil
}

// piCommand is the pi executable a thread runs: the one its configuration or
// JAZ_PI_COMMAND names, else the pi on the server's PATH.
func piCommand(cfg AgentConfig) string {
	if command := strings.TrimSpace(cfg.Env["JAZ_PI_COMMAND"]); command != "" {
		return command
	}
	resolved, err := ResolveExecutable(AgentPi)
	if err != nil {
		return ""
	}
	return resolved
}

// ProbePiEnv is the environment a pi process for this agent runs with. It is
// the same set a thread uses, so a Settings probe reads the real profile.
func (m *Manager) ProbePiEnv(agent string) (map[string]string, error) {
	cfg, ok, err := m.configuredAgent(agent)
	if err != nil || !ok {
		return nil, err
	}
	return m.processEnvPrepared(agent, cfg)
}
