package acp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	acpschema "github.com/gluonfield/acp-transport/acp"
	"github.com/wins/jaz/backend/internal/modelcatalog"
)

// devinModels reads the models Devin offers the signed-in account from a
// throwaway session's config options. Devin lists the model and its thought
// levels itself, so an account that offers none reports an empty catalogue
// rather than a guessed list.
func (m *Manager) devinModels(ctx context.Context, cfg AgentConfig) (AgentModelCatalog, error) {
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cwd := filepath.Join(firstNonEmpty(m.cfg.Root, filepath.Join(os.TempDir(), "jaz")), "acp", "model-discovery")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		return AgentModelCatalog{}, err
	}
	ac, err := m.connect(deadline, AgentDevin, cfg, cwd, "", "", nil)
	if err != nil {
		return AgentModelCatalog{}, err
	}
	defer ac.close()
	raw, err := ac.peer.Call(deadline, "session/new", newSessionRequest{Cwd: cwd, MCPServers: []json.RawMessage{}})
	if err != nil {
		return AgentModelCatalog{}, err
	}
	var session struct {
		SessionID acpschema.SessionID `json:"sessionId"`
		Options   []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
			Options  []struct {
				Value string `json:"value"`
				Name  string `json:"name"`
			} `json:"options"`
		} `json:"configOptions"`
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		return AgentModelCatalog{}, err
	}
	closeSession(deadline, ac, session.SessionID)
	state := parseSessionConfigOptions(raw)
	catalog := AgentModelCatalog{Models: []AgentModel{}, DefaultModel: state.currentModel}
	for _, option := range session.Options {
		if option.Category != "model" && option.ID != sessionConfigModel {
			continue
		}
		for _, choice := range option.Options {
			model := AgentModel{Value: choice.Value, Label: choice.Name,
				Reasoning: ReasoningCapabilities{Status: modelcatalog.ReasoningUnavailable, Scope: ReasoningScopeAgent}}
			if len(state.effortOptions) > 0 {
				model.Reasoning.Status = modelcatalog.ReasoningReady
				model.Reasoning.Efforts = state.effortOptions
			}
			catalog.Models = append(catalog.Models, model)
		}
	}
	return catalog, nil
}

// closeSession releases the native session a discovery probe opened, so an
// unused session does not outlive the probe.
func closeSession(ctx context.Context, ac *agentConn, sessionID acpschema.SessionID) {
	if sessionID == "" {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if _, err := ac.peer.Call(cleanup, acpschema.AgentMethodSessionClose, acpschema.CloseSessionRequest{SessionID: sessionID}); err != nil {
		_, _ = ac.peer.Call(cleanup, "session/delete", map[string]acpschema.SessionID{"sessionId": sessionID})
	}
}
