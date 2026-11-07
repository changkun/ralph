// Package transcript reads the JSONL event streams that agent CLIs print,
// Claude Code's --output-format stream-json and Codex's --json, into one
// common form for display.
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Kind classifies an Event.
type Kind string

// Event kinds.
const (
	Info       Kind = "info"        // session details, such as the model
	Message    Kind = "message"     // text the agent wrote
	Thinking   Kind = "thinking"    // the agent's visible reasoning
	ToolCall   Kind = "tool"        // a tool call, such as Read or Bash
	ToolResult Kind = "tool-result" // what a tool call returned
	Command    Kind = "command"     // a shell command and its output
	FileChange Kind = "file-change" // files the agent changed
	Error      Kind = "error"       // an error the CLI reported
	Result     Kind = "result"      // the end of the turn
)

// Event is one step of an agent's turn.
type Event struct {
	Kind   Kind
	Title  string // one-line summary, such as a tool name and its argument
	Text   string // details, such as message text or command output
	Failed bool   // for tool results, commands, and results: whether it failed
}

// Turn collects the events of one agent turn, one stream line at a time,
// and works out how the turn ended.
type Turn struct {
	Events []Event

	result      string
	hasResult   bool
	lastMessage string
	failure     string
}

// Add parses one line of a Claude Code or Codex stream, records its events,
// and returns them. Lines it does not recognize yield no events.
func (t *Turn) Add(line []byte) []Event {
	events := Parse(line)
	for _, e := range events {
		switch e.Kind {
		case Message:
			t.lastMessage = e.Text
		case Result:
			if e.Failed {
				t.failure = e.Text
			} else if e.Text != "" {
				t.result, t.hasResult = e.Text, true
			}
		}
	}
	t.Events = append(t.Events, events...)
	return events
}

// Result returns the agent's final answer: the result the CLI reported, or
// else the last message the agent wrote.
func (t *Turn) Result() string {
	if t.hasResult {
		return t.result
	}
	return t.lastMessage
}

// Failure returns the reason the CLI gave for a failed turn, or "" if it
// reported none.
func (t *Turn) Failure() string { return t.failure }

// ReadFile returns the events of a saved stream, such as a turn's .jsonl
// round log. It ignores a last line without a newline, which the CLI may
// still be writing.
func ReadFile(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Turn
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		for line := range bytes.Lines(data[:i+1]) {
			if line = bytes.TrimSpace(line); len(line) > 0 {
				t.Add(line)
			}
		}
	}
	return t.Events, nil
}

// Parse returns the events in one line of a Claude Code or Codex stream.
func Parse(line []byte) []Event {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &head) != nil {
		return nil
	}
	switch head.Type {
	case "system", "assistant", "user", "result":
		return parseClaude(head.Type, line)
	default:
		return parseCodex(head.Type, line)
	}
}

// claudeEvent is the subset of a Claude Code stream-json event that Parse
// reads.
type claudeEvent struct {
	Subtype string `json:"subtype"`
	// system init
	Model   string `json:"model"`
	Version string `json:"claude_code_version"`
	// assistant and user
	Message struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	// result
	IsError  bool     `json:"is_error"`
	Result   string   `json:"result"`
	Errors   []string `json:"errors"`
	CostUSD  float64  `json:"total_cost_usd"`
	NumTurns int      `json:"num_turns"`
	Duration int      `json:"duration_ms"`
}

type claudeBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"`
	IsError  bool            `json:"is_error"`
}

func parseClaude(typ string, line []byte) []Event {
	var e claudeEvent
	if json.Unmarshal(line, &e) != nil {
		return nil
	}
	switch typ {
	case "system":
		if e.Subtype != "init" {
			return nil // hooks, token estimates, and other bookkeeping
		}
		return []Event{{Kind: Info, Title: strings.TrimSpace("Claude Code " + e.Version + " · " + e.Model)}}
	case "result":
		title := fmt.Sprintf("Finished in %s · %d turns · $%.4f", seconds(e.Duration), e.NumTurns, e.CostUSD)
		if e.IsError {
			text := e.Result
			if text == "" {
				text = strings.Join(e.Errors, "\n")
			}
			if text == "" {
				text = e.Subtype
			}
			return []Event{{Kind: Result, Title: "Failed · " + title, Text: text, Failed: true}}
		}
		return []Event{{Kind: Result, Title: title, Text: e.Result}}
	}
	// assistant or user: a list of content blocks, or a plain string
	var blocks []claudeBlock
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return nil
	}
	var events []Event
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				events = append(events, Event{Kind: Message, Text: b.Text})
			}
		case "thinking":
			if strings.TrimSpace(b.Thinking) != "" {
				events = append(events, Event{Kind: Thinking, Text: b.Thinking})
			}
		case "tool_use":
			events = append(events, Event{Kind: ToolCall, Title: toolTitle(b.Name, b.Input), Text: indent(b.Input)})
		case "tool_result":
			events = append(events, Event{Kind: ToolResult, Text: toolResultText(b.Content), Failed: b.IsError})
		}
	}
	return events
}

// toolTitle summarizes a tool call by its name and most telling argument.
func toolTitle(name string, input json.RawMessage) string {
	var args map[string]any
	_ = json.Unmarshal(input, &args)
	for _, key := range []string{"command", "file_path", "path", "pattern", "url", "query", "description"} {
		if s, ok := args[key].(string); ok && s != "" {
			return name + ": " + firstLine(s)
		}
	}
	return name
}

// toolResultText flattens a tool result, which is a string or a list of
// content blocks.
func toolResultText(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}
	var blocks []claudeBlock
	if json.Unmarshal(content, &blocks) != nil {
		return string(content)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		} else {
			parts = append(parts, "["+b.Type+"]")
		}
	}
	return strings.Join(parts, "\n")
}

// codexEvent is the subset of a Codex --json event that Parse reads.
type codexEvent struct {
	Message string `json:"message"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
	Usage struct {
		Input  int `json:"input_tokens"`
		Cached int `json:"cached_input_tokens"`
		Output int `json:"output_tokens"`
	} `json:"usage"`
	Item struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Message  string `json:"message"`
		Command  string `json:"command"`
		Output   string `json:"aggregated_output"`
		ExitCode *int   `json:"exit_code"`
		Status   string `json:"status"`
		Server   string `json:"server"`
		Tool     string `json:"tool"`
		Query    string `json:"query"`
		Changes  []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"changes"`
		Items []struct {
			Text      string `json:"text"`
			Completed bool   `json:"completed"`
		} `json:"items"`
	} `json:"item"`
}

func parseCodex(typ string, line []byte) []Event {
	var e codexEvent
	if json.Unmarshal(line, &e) != nil {
		return nil
	}
	switch typ {
	case "thread.started":
		return []Event{{Kind: Info, Title: "Codex session started"}}
	case "turn.completed":
		return []Event{{Kind: Result, Title: fmt.Sprintf("Finished · %d input tokens (%d cached) · %d output tokens",
			e.Usage.Input, e.Usage.Cached, e.Usage.Output)}}
	case "turn.failed":
		return []Event{{Kind: Result, Title: "Failed", Text: e.Error.Message, Failed: true}}
	case "error":
		return []Event{{Kind: Error, Text: e.Message}}
	case "item.completed":
		return codexItem(e)
	}
	return nil // turn.started, item.started, item.updated
}

func codexItem(e codexEvent) []Event {
	it := e.Item
	switch it.Type {
	case "agent_message":
		return []Event{{Kind: Message, Text: it.Text}}
	case "reasoning":
		return []Event{{Kind: Thinking, Text: it.Text}}
	case "command_execution":
		failed := it.Status == "failed" || (it.ExitCode != nil && *it.ExitCode != 0)
		title := firstLine(it.Command)
		if it.ExitCode != nil && *it.ExitCode != 0 {
			title += fmt.Sprintf(" (exit %d)", *it.ExitCode)
		}
		return []Event{{Kind: Command, Title: title, Text: it.Output, Failed: failed}}
	case "file_change":
		var paths []string
		for _, c := range it.Changes {
			paths = append(paths, c.Kind+" "+c.Path)
		}
		return []Event{{Kind: FileChange, Title: strings.Join(paths, ", "), Failed: it.Status == "failed"}}
	case "mcp_tool_call":
		return []Event{{Kind: ToolCall, Title: it.Server + ": " + it.Tool, Failed: it.Status == "failed"}}
	case "web_search":
		return []Event{{Kind: ToolCall, Title: "Web search: " + it.Query}}
	case "todo_list":
		var lines []string
		for _, todo := range it.Items {
			mark := "[ ]"
			if todo.Completed {
				mark = "[x]"
			}
			lines = append(lines, mark+" "+todo.Text)
		}
		return []Event{{Kind: Info, Title: "Plan", Text: strings.Join(lines, "\n")}}
	case "error":
		return []Event{{Kind: Error, Text: it.Message}}
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func indent(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	// Encode without HTML escapes, so that a command like "a > b" reads
	// as written.
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if enc.Encode(v) != nil {
		return string(raw)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func seconds(ms int) string { return fmt.Sprintf("%.1fs", float64(ms)/1000) }
