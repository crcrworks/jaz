package pirpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFakePiProcess is the pi process the bridge tests drive: it speaks pi's
// own RPC records, so the bridge is exercised against the shapes pi documents.
func TestFakePiProcess(t *testing.T) {
	if os.Getenv("JAZ_FAKE_PI") != "1" {
		return
	}
	if path := os.Getenv("JAZ_FAKE_PI_LAUNCH_LOG"); path != "" {
		record, _ := json.Marshal(os.Getenv("JAZ_FAKE_PI_ARGS"))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintln(file, string(record))
			_ = file.Close()
		}
	}
	out := bufio.NewWriter(os.Stdout)
	emit := func(record any) {
		data, _ := json.Marshal(record)
		_, _ = out.Write(append(data, '\n'))
		_ = out.Flush()
	}
	respond := func(command string, id json.RawMessage, data any, failure string) {
		record := map[string]any{"id": json.RawMessage(id), "type": "response", "command": command, "success": failure == ""}
		if failure != "" {
			record["error"] = failure
		} else {
			record["data"] = data
		}
		emit(record)
	}
	model := map[string]any{
		"id": "fake-model", "name": "Fake Model", "provider": "fake",
		"contextWindow": 128000, "maxTokens": 4096, "reasoning": true,
		"input":           []string{"text"},
		"thinkingLevelMap": map[string]any{"off": nil, "low": "low", "high": "high"},
	}
	thinking := "high"
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return
		}
		var command struct {
			ID      json.RawMessage `json:"id"`
			Type    string          `json:"type"`
			Message string          `json:"message"`
			Level   string          `json:"level"`
		}
		if json.Unmarshal(line, &command) != nil {
			continue
		}
		switch command.Type {
		case "get_state":
			respond(command.Type, command.ID, map[string]any{"model": model, "thinkingLevel": thinking}, "")
		case "get_available_models":
			respond(command.Type, command.ID, map[string]any{"models": []any{model}}, "")
		case "get_available_thinking_levels":
			respond(command.Type, command.ID, map[string]any{"levels": []string{"low", "high"}}, "")
		case "set_model", "compact":
			respond(command.Type, command.ID, map[string]any{}, "")
		case "set_thinking_level":
			thinking = command.Level
			respond(command.Type, command.ID, map[string]any{}, "")
		case "prompt":
			respond(command.Type, command.ID, map[string]any{"disposition": "started"}, "")
			emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "fake says " + command.Message}})
			emit(map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "thinking_delta", "delta": "fake thinking"}})
			emit(map[string]any{"type": "tool_execution_start", "toolCallId": "call-1", "toolName": "bash", "args": map[string]any{"command": "echo hi"}})
			emit(map[string]any{"type": "tool_execution_end", "toolCallId": "call-1", "toolName": "bash", "isError": false, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": "hi"}}}})
			emit(map[string]any{"type": "agent_settled", "aborted": false})
		default:
			respond(command.Type, command.ID, nil, "Unknown command: "+command.Type)
		}
	}
}

// fakeLaunch is the pi executable a bridge test runs: a script that starts this
// test binary as the fake pi process, so the bridge launches it exactly as it
// launches pi.
func fakeLaunch(t *testing.T, logPath string) Launch {
	t.Helper()
	script := filepath.Join(t.TempDir(), "pi")
	// pi's own arguments are recorded and dropped: this test binary only takes
	// its own flags.
	body := "#!/bin/sh\nJAZ_FAKE_PI_ARGS=\"$*\" exec " + shellQuote(os.Args[0]) + " -test.run=^TestFakePiProcess$\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"PATH": os.Getenv("PATH"), "JAZ_FAKE_PI": "1"}
	if logPath != "" {
		env["JAZ_FAKE_PI_LAUNCH_LOG"] = logPath
	}
	return Launch{Command: script, Env: env}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
