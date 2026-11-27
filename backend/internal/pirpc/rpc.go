// Package pirpc speaks pi's native JSONL RPC mode: one process per session,
// commands on stdin, responses and session events on stdout.
package pirpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/wins/jaz/backend/internal/processenv"
	"github.com/wins/jaz/backend/internal/subprocess"
)

// Launch is everything a pi process needs to start: its command line, the
// environment that names its isolated profile, and the thread's directory.
type Launch struct {
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
}

// Event is one session event pi emitted, such as message_update or
// agent_settled. Data is the record's own JSON.
type Event struct {
	Type string
	Data json.RawMessage
}

type response struct {
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
}

type Client struct {
	stdin io.WriteCloser
	close func(error)
	done  chan struct{}

	mu      sync.Mutex
	nextID  int
	pending map[string]chan response

	settleMu  sync.Mutex
	settles   chan settle
	lastSettle settle

	events chan Event
	err    error
}

// settle is how one pi run ended, as the last agent_settled reported it.
type settle struct {
	Aborted bool
	Err     error
}

// Start spawns pi in RPC mode and reads its records until the process exits.
func Start(ctx context.Context, launch Launch) (*Client, error) {
	cmd := exec.CommandContext(ctx, launch.Command, launch.Args...)
	process := subprocess.New(cmd)
	cmd.Env = processenv.List(launch.Env)
	if launch.Cwd != "" {
		cmd.Dir = launch.Cwd
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &stderrTail{}
	cmd.Stderr = io.MultiWriter(os.Stderr, stderr)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start pi: %w", err)
	}
	if err := process.Started(); err != nil {
		_ = process.Terminate()
		return nil, fmt.Errorf("prepare pi process: %w", err)
	}
	c := &Client{
		stdin:   stdin,
		done:    make(chan struct{}),
		pending: map[string]chan response{},
		settles: make(chan settle, 1),
		events:  make(chan Event, 64),
	}
	var once sync.Once
	c.close = func(error) {
		once.Do(func() {
			_ = stdin.Close()
			_ = process.Terminate()
			close(c.done)
		})
	}
	go func() {
		c.read(bufio.NewReaderSize(stdout, 1<<20))
		if err := cmd.Wait(); err != nil {
			stderr.mu.Lock()
			detail := strings.TrimSpace(stderr.text)
			stderr.mu.Unlock()
			if detail != "" {
				err = fmt.Errorf("%w: %s", err, detail)
			}
		}
		c.fail(err)
	}()
	return c, nil
}

func (c *Client) read(r *bufio.Reader) {
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			c.dispatch(line)
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) dispatch(line []byte) {
	var record struct {
		ID      json.RawMessage `json:"id"`
		Type    string          `json:"type"`
		Command string          `json:"command"`
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal(line, &record) != nil || record.Type == "" {
		return
	}
	// pi answers a malformed command with a "parse" response and no id.
	if record.Type == "response" && len(record.ID) > 0 {
		key := strings.Trim(string(record.ID), `"`)
		c.mu.Lock()
		waiter := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		if waiter != nil {
			waiter <- response{Command: record.Command, Success: record.Success, Data: record.Data, Error: record.Error}
		}
		return
	}
	if record.Type == "agent_settled" {
		var settled struct {
			Aborted bool `json:"aborted"`
		}
		_ = json.Unmarshal(line, &settled)
		c.settleMu.Lock()
		c.lastSettle = settle{Aborted: settled.Aborted}
		c.settleMu.Unlock()
		select {
		case c.settles <- c.lastSettle:
		default:
		}
		return
	}
	select {
	case c.events <- Event{Type: record.Type, Data: json.RawMessage(line)}:
	case <-c.done:
	}
}

// drain forgets runs that finished before the turn asking for one began.
func (c *Client) drain() {
	for {
		select {
		case <-c.settles:
		default:
			return
		}
	}
}

// settled waits for pi to say it has no automatic work left: the end of one
// turn, including the retries, overflow recovery and compaction pi does itself.
func (c *Client) settled(ctx context.Context) settle {
	select {
	case result := <-c.settles:
		c.settleMu.Lock()
		if c.err != nil {
			result.Err = c.err
		}
		c.settleMu.Unlock()
		return result
	case <-c.done:
		c.settleMu.Lock()
		defer c.settleMu.Unlock()
		if c.err != nil {
			return settle{Err: c.err}
		}
		return settle{}
	case <-ctx.Done():
		return settle{Err: ctx.Err()}
	}
}

// Call runs one command and decodes its data into out. A failed command is an
// error carrying pi's own message.
func (c *Client) Call(ctx context.Context, command string, params map[string]any, out any) error {
	raw, err := c.call(ctx, command, params)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) call(ctx context.Context, command string, params map[string]any) (json.RawMessage, error) {
	payload := map[string]any{"type": command}
	for key, value := range params {
		payload[key] = value
	}
	c.mu.Lock()
	c.nextID++
	id := fmt.Sprintf("%d", c.nextID)
	payload["id"] = id
	waiter := make(chan response, 1)
	c.pending[id] = waiter
	c.mu.Unlock()
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("send pi command %s: %w", command, err)
	}
	select {
	case reply := <-waiter:
		if !reply.Success {
			return nil, &CommandError{Command: command, Message: reply.Error}
		}
		return reply.Data, nil
	case <-c.done:
		c.mu.Lock()
		delete(c.pending, id)
		settled := c.err
		c.mu.Unlock()
		if settled != nil {
			return nil, fmt.Errorf("pi exited: %w", settled)
		}
		return nil, errors.New("pi exited")
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// CommandError reports a command pi refused, with pi's own words.
type CommandError struct {
	Command string
	Message string
}

func (e *CommandError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("pi refused %s", e.Command)
	}
	return e.Message
}

func (c *Client) fail(err error) {
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
	c.close(nil)
}

// Events delivers session events until the process closes the channel.
func (c *Client) Events() <-chan Event {
	return c.events
}

// Err reports why the process ended, when it was not a clean shutdown.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Client) Close() {
	c.close(nil)
}

type stderrTail struct {
	mu    sync.Mutex
	text  string
	limit int
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.limit == 0 {
		t.limit = 2000
	}
	t.text += string(p)
	if len(t.text) > t.limit {
		t.text = t.text[len(t.text)-t.limit:]
	}
	return len(p), nil
}

