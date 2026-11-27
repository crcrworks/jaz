package pirpc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	acpschema "github.com/gluonfield/acp-transport/acp"
	"github.com/gluonfield/acp-transport/jsonrpc"
)

// Config is everything the bridge needs to start one thread's pi process.
type Config struct {
	Command string
	Env     map[string]string
	Profile Profile
	// Prompt is Jaz's session extension: platform context, prompt files,
	// connections, memory and skills. pi appends it to its own prompt.
	Prompt  func(context.Context, string) (string, error)
	Servers func(context.Context) ([]MCPServer, error)
}

// OpenACPConn answers ACP over a pipe while pi speaks its own RPC on a child
// process: the adapter layer that keeps pi's record shapes out of the manager.
// The manager opens the connection before it knows the thread's session id, so
// the first ACP session call tells the bridge which pi session to run.
func OpenACPConn(ctx context.Context, cfg Config) jsonrpc.MessageConn {
	clientConn, serverConn := jsonrpc.Pipe(64)
	// A thread's pi process outlives the call that opened it, so it runs on its
	// own context, ended when the bridge itself ends.
	runCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	bridge := &acpBridge{cfg: cfg, sessions: map[string]*bridgeSession{}, runCtx: runCtx, stop: stop}
	bridge.peer = jsonrpc.NewPeer(serverConn, bridge)
	go func() {
		defer serverConn.Close()
		defer stop()
		_ = bridge.peer.Serve(ctx)
	}()
	return clientConn
}

type acpBridge struct {
	cfg  Config
	peer *jsonrpc.Peer

	runCtx context.Context
	stop   context.CancelFunc

	mu       sync.Mutex
	sessions map[string]*bridgeSession
}

type bridgeSession struct {
	id     string
	client *Client
	// model and effort are what ACP last selected; they are applied to the pi
	// process once its state is known.
	model  string
	effort string
}

func (b *acpBridge) HandleJSONRPC(ctx context.Context, req jsonrpc.Request) (json.RawMessage, *jsonrpc.Error) {
	switch req.Method {
	case acpschema.AgentMethodInitialize:
		return b.handleInitialize()
	case acpschema.AgentMethodSessionNew:
		return b.handleSessionNew(ctx, req.Params)
	case acpschema.AgentMethodSessionResume, acpschema.AgentMethodSessionLoad:
		return b.handleSessionResume(ctx, req.Params)
	case acpschema.AgentMethodSessionPrompt:
		return b.handlePrompt(ctx, req.Params)
	case acpschema.AgentMethodSessionCancel:
		return b.handleCancel(req.Params)
	case acpschema.AgentMethodSessionSetConfigOption:
		return b.handleSetConfigOption(ctx, req.Params)
	default:
		return nil, jsonrpc.MethodNotFound(req.Method)
	}
}

func (b *acpBridge) handleInitialize() (json.RawMessage, *jsonrpc.Error) {
	return jsonrpc.EncodeResult(acpschema.InitializeResponse{
		ProtocolVersion: acpschema.ProtocolVersion(acpschema.ProtocolVersionNumber),
		AgentInfo: &acpschema.Implementation{
			Name:    AgentName,
			Title:   DisplayName,
			Version: Version,
		},
		AgentCapabilities: &acpschema.AgentCapabilities{
			LoadSession: true,
			SessionCapabilities: &acpschema.SessionCapabilities{
				Resume: &acpschema.SessionResumeCapabilities{},
			},
			MCPCapabilities: &acpschema.MCPCapabilities{
				// Jaz hands pi its servers through pi's own MCP client, not this
				// field, so Jaz must not believe they travelled here.
				HTTP: false,
				SSE:  false,
			},
			PromptCapabilities: &acpschema.PromptCapabilities{
				Image:           true,
				Audio:           false,
				EmbeddedContext: false,
			},
		},
	})
}



func (b *acpBridge) handleSessionNew(ctx context.Context, raw json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
	var req struct {
		Cwd  string         `json:"cwd"`
		Meta map[string]any `json:"_meta"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, jsonrpc.InvalidParams("invalid session/new request", nil)
		}
	}
	session, err := b.start(ctx, sessionMetaPrompt(req.Meta), newSessionID(), req.Cwd)
	if err != nil {
		return nil, mapBridgeError(err)
	}
	return jsonrpc.EncodeResult(map[string]any{
		"sessionId":     session.id,
		"configOptions": session.configOptions(ctx),
	})
}

func (b *acpBridge) handleSessionResume(ctx context.Context, raw json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
	var req struct {
		SessionID acpschema.SessionID `json:"sessionId"`
		Cwd       string              `json:"cwd"`
		Meta      map[string]any      `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &req); err != nil || req.SessionID == "" {
		return nil, jsonrpc.InvalidParams("invalid session/resume request", nil)
	}
	if _, err := b.start(ctx, sessionMetaPrompt(req.Meta), string(req.SessionID), req.Cwd); err != nil {
		return nil, mapBridgeError(err)
	}
	return jsonrpc.EncodeResult(acpschema.LoadSessionResponse{})
}

// start launches the thread's pi process once. It reopens the session named by
// sessionID, so a resumed thread keeps its provider history.
func (b *acpBridge) start(ctx context.Context, metaPrompt, sessionID, cwd string) (*bridgeSession, error) {
	if session := b.session(sessionID); session != nil {
		return session, nil
	}
	seeded := launchContext(ctx)
	cwd = firstNonEmpty(cwd, seeded.Cwd)
	prompt := firstNonEmpty(metaPrompt, seeded.Prompt)
	var servers []MCPServer
	if b.cfg.Servers != nil {
		listed, err := b.cfg.Servers(ctx)
		if err != nil {
			return nil, err
		}
		servers = listed
	}
	launch, err := b.cfg.Profile.launch(b.cfg, sessionID, cwd, prompt, servers)
	if err != nil {
		return nil, err
	}
	client, err := Start(b.runCtx, launch)
	if err != nil {
		return nil, err
	}
	session := &bridgeSession{id: sessionID, client: client}
	go b.pump(session)
	b.mu.Lock()
	b.sessions[sessionID] = session
	b.mu.Unlock()
	return session, nil
}

// pump forwards pi's session events as ACP session updates for this session.
func (b *acpBridge) pump(session *bridgeSession) {
	for event := range session.client.Events() {
		if update := piEvent(event.Type, event.Data); update != nil {
			b.notify(session, update)
		}
	}
}

func (b *acpBridge) notify(session *bridgeSession, update any) {
	_ = b.peer.Notify(context.Background(), acpschema.ClientMethodSessionUpdate, map[string]any{
		"sessionId": session.id,
		"update":    update,
	})
}

// handlePrompt runs one turn: pi's prompt, or its own compaction when the
// manager asks for a compact.
func (b *acpBridge) handlePrompt(ctx context.Context, raw json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
	req, rpcErr := jsonrpc.DecodeParams[acpschema.PromptRequest](raw)
	if rpcErr != nil {
		return nil, rpcErr
	}
	session := b.session(string(req.SessionID))
	if session == nil {
		return nil, jsonrpc.InvalidParams("unknown session", nil)
	}
	message, err := promptText(req.Prompt)
	if err != nil {
		return nil, jsonrpc.InvalidParams(err.Error(), nil)
	}
	if isCompactPrompt(message) {
		return b.compact(ctx, session)
	}
	session.client.drain()
	if err := session.client.Call(ctx, "prompt", map[string]any{"message": message}, nil); err != nil {
		return nil, mapBridgeError(err)
	}
	settled := session.client.settled(ctx)
	if settled.Err != nil {
		return nil, jsonrpc.InternalError(settled.Err.Error(), nil)
	}
	return jsonrpc.EncodeResult(acpschema.PromptResponse{StopReason: stopReason(settled.Aborted)})
}

// compact runs pi's own compaction. Jaz never writes a summary of its own.
func (b *acpBridge) compact(ctx context.Context, session *bridgeSession) (json.RawMessage, *jsonrpc.Error) {
	var result struct {
		TokensBefore int `json:"tokensBefore"`
	}
	if err := session.client.Call(ctx, "compact", nil, &result); err != nil {
		var refused *CommandError
		if errors.As(err, &refused) {
			return nil, jsonrpc.InternalError("pi could not compact this thread: "+refused.Message, nil)
		}
		return nil, mapBridgeError(err)
	}
	// A compaction is pi's own run with its own start and end events; the
	// command's answer is the whole of it, so no agent_settled follows.
	return jsonrpc.EncodeResult(acpschema.PromptResponse{StopReason: acpschema.StopReasonEndTurn})
}

func stopReason(aborted bool) acpschema.StopReason {
	if aborted {
		return acpschema.StopReasonCancelled
	}
	return acpschema.StopReasonEndTurn
}

func (b *acpBridge) handleCancel(raw json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
	var req acpschema.CancelNotification
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, jsonrpc.InvalidParams("invalid session/cancel", nil)
	}
	if session := b.session(string(req.SessionID)); session != nil {
		_ = session.client.Call(context.Background(), "abort", nil, nil)
	}
	return jsonrpc.EncodeResult(map[string]any{})
}

func (b *acpBridge) handleSetConfigOption(ctx context.Context, raw json.RawMessage) (json.RawMessage, *jsonrpc.Error) {
	var req struct {
		SessionID acpschema.SessionID `json:"sessionId"`
		ConfigID  string              `json:"configId"`
		Value     json.RawMessage     `json:"value"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, jsonrpc.InvalidParams("invalid session/set_config_option request", nil)
	}
	var value string
	if err := json.Unmarshal(req.Value, &value); err != nil {
		return nil, jsonrpc.InvalidParams("invalid config option value", nil)
	}
	session := b.session(string(req.SessionID))
	if session == nil {
		return nil, jsonrpc.InvalidParams("unknown session", nil)
	}
	options, err := session.set(ctx, req.ConfigID, value)
	if err != nil {
		return nil, mapBridgeError(err)
	}
	return jsonrpc.EncodeResult(map[string]any{"configOptions": options})
}

func (b *acpBridge) session(id string) *bridgeSession {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[id]
}

func mapBridgeError(err error) *jsonrpc.Error {
	var command *CommandError
	if errors.As(err, &command) {
		return jsonrpc.InvalidParams(err.Error(), nil)
	}
	return jsonrpc.InternalError(err.Error(), nil)
}

