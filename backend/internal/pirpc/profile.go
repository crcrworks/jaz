package pirpc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/wins/jaz/backend/internal/runtimefiles"
)

// Profile is pi's isolated home and the per-session files one Jaz thread hands
// its own pi process. The home is shared so one `pi auth login` serves every
// thread; everything that names a thread lives under Sessions.
type Profile struct {
	Home     string
	Sessions string
}

func NewProfile(root string) Profile {
	home := runtimefiles.New(root).ACPPiHome
	return Profile{Home: home, Sessions: filepath.Join(home, "sessions")}
}

type sessionFiles struct {
	// Session is the pi session file the thread's history lives in. pi reopens
	// it from this path, so a restart keeps the conversation and its cwd.
	Session   string
	Prompt    string
	Extension string
}

func (p Profile) sessionFiles(id string) sessionFiles {
	dir := filepath.Join(p.Sessions, safeSessionName(id))
	return sessionFiles{
		Session:   filepath.Join(dir, "session.jsonl"),
		Prompt:    filepath.Join(dir, "append-prompt.md"),
		Extension: filepath.Join(dir, "mcp.ts"),
	}
}

// safeSessionName keeps a Jaz session id usable as one path segment.
func safeSessionName(id string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(id) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	if name := strings.Trim(out.String(), ".-"); name != "" {
		return name
	}
	return "session"
}

// MCPServer is one of Jaz's MCP servers as pi's client needs it.
type MCPServer struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

const mcpToolNameNote = `Jaz's own tools reach you over MCP and are named mcp__<server>__<tool>: mcp__jaztools__ask_user, mcp__jaztools__create_thread, mcp__jaztools__send_message_to_thread, mcp__jaztools__wait_threads, mcp__jaztools__stop_thread, mcp__jaztools__list_agent_options. Where this prompt names ask_user or create_thread, that tool is the one named above.`

// launch writes the thread's prompt and MCP registration and returns the pi
// command line that names them. pi owns its prompt and tools, so Jaz appends
// its instructions to pi's own prompt and registers its servers with pi's own
// MCP client instead of replacing either.
func (p Profile) launch(cfg Config, sessionID, cwd, systemPrompt string, servers []MCPServer) (Launch, error) {
	files := p.sessionFiles(sessionID)
	if err := os.MkdirAll(filepath.Dir(files.Session), 0o700); err != nil {
		return Launch{}, err
	}
	args := []string{"--mode", "rpc", "--session", files.Session, "--no-approve", "--no-prompt-templates"}
	prompt := strings.TrimSpace(systemPrompt)
	if len(servers) > 0 {
		prompt = strings.TrimSpace(prompt + "\n\n" + mcpToolNameNote)
		extension, err := mcpExtension(servers)
		if err != nil {
			return Launch{}, err
		}
		if err := os.WriteFile(files.Extension, []byte(extension), 0o600); err != nil {
			return Launch{}, err
		}
		args = append(args, "--extension", files.Extension)
	}
	if prompt != "" {
		if err := os.WriteFile(files.Prompt, []byte(prompt), 0o600); err != nil {
			return Launch{}, err
		}
		args = append(args, "--append-system-prompt", files.Prompt)
	}
	return Launch{Command: cfg.Command, Args: args, Env: cfg.Env, Cwd: cwd}, nil
}

// mcpServerName reports whether pi accepts a server name: letters, digits, "_"
// and "-", and no blank.
func mcpServerName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// mcpExtension registers Jaz's servers with pi's built-in MCP client. They are
// `direct` because Jaz's platform prompt names their tools outright.
func mcpExtension(servers []MCPServer) (string, error) {
	lines := make([]string, 0, len(servers))
	for _, server := range servers {
		if strings.TrimSpace(server.URL) == "" || !mcpServerName(server.Name) {
			continue
		}
		config, err := json.Marshal(map[string]any{
			"url":         server.URL,
			"headers":     server.Headers,
			"exposure":    "direct",
			"description": "Jaz thread tools: threads, questions, widgets, memory, connected accounts and the browser.",
		})
		if err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("  pi.registerMcpServer(%q, %s);", server.Name, config))
	}
	return "export default function (pi: any) {\n" + strings.Join(lines, "\n") + "\n}\n", nil
}
