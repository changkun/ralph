package transcript

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// load feeds a captured stream from testdata into a Turn.
func load(t *testing.T, name string) *Turn {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var turn Turn
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		turn.Add(sc.Bytes())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return &turn
}

func kinds(events []Event) []Kind {
	var ks []Kind
	for _, e := range events {
		ks = append(ks, e.Kind)
	}
	return ks
}

func assertKinds(t *testing.T, got []Event, want ...Kind) {
	t.Helper()
	if g := kinds(got); strings.Join(toStrings(g), ",") != strings.Join(toStrings(want), ",") {
		t.Errorf("kinds:\n got %v\nwant %v", g, want)
	}
}

func toStrings(ks []Kind) []string {
	var s []string
	for _, k := range ks {
		s = append(s, string(k))
	}
	return s
}

func TestClaudeStream(t *testing.T) {
	turn := load(t, "claude.jsonl")
	assertKinds(t, turn.Events, Info, ToolCall, ToolResult, Message, Result)
	if got := turn.Events[0].Title; got != "Claude Code 2.1.319 · claude-haiku-5-5" {
		t.Errorf("info title = %q", got)
	}
	if got := turn.Events[1].Title; !strings.HasPrefix(got, "Bash: printf 'hi' > hello.txt") {
		t.Errorf("tool title = %q", got)
	}
	if got := turn.Events[2].Text; got != "hi" {
		t.Errorf("tool result = %q", got)
	}
	if got := turn.Result(); got != "done" {
		t.Errorf("Result() = %q, want %q", got, "done")
	}
	if got := turn.Failure(); got != "" {
		t.Errorf("Failure() = %q, want empty", got)
	}
	if got := turn.Events[4].Title; !strings.Contains(got, "$0.0011") {
		t.Errorf("result title = %q, want the cost", got)
	}
}

func TestCodexStream(t *testing.T) {
	turn := load(t, "codex.jsonl")
	assertKinds(t, turn.Events, Info, Command, Message, Result)
	cmd := turn.Events[1]
	if cmd.Title != "/bin/bash -lc 'echo hi > codex.txt'" || cmd.Failed {
		t.Errorf("command = %+v", cmd)
	}
	if got := turn.Result(); got != "done" {
		t.Errorf("Result() = %q, want the last message %q", got, "done")
	}
	if !strings.Contains(turn.Events[3].Title, "output tokens") {
		t.Errorf("result title = %q", turn.Events[3].Title)
	}
}

func TestCodexFailedStream(t *testing.T) {
	turn := load(t, "codex-failed.jsonl")
	assertKinds(t, turn.Events, Info, Error, Result)
	if got := turn.Failure(); !strings.Contains(got, "not supported") {
		t.Errorf("Failure() = %q", got)
	}
	if got := turn.Result(); got != "" {
		t.Errorf("Result() = %q, want empty", got)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []Event
	}{
		{"not json", `progress…`, nil},
		{"claude hook", `{"type":"system","subtype":"hook_started"}`, nil},
		{"claude thinking", `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"}]}}`,
			[]Event{{Kind: Thinking, Text: "hmm"}}},
		{"claude empty thinking", `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":""}]}}`, nil},
		{"claude user prompt string", `{"type":"user","message":{"content":"hello"}}`, nil},
		{"claude tool title from file_path", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/work/main.go"}}]}}`,
			[]Event{{Kind: ToolCall, Title: "Read: /work/main.go", Text: "{\n  \"file_path\": \"/work/main.go\"\n}"}}},
		{"claude failed tool result blocks", `{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,"content":[{"type":"text","text":"no such file"}]}]}}`,
			[]Event{{Kind: ToolResult, Text: "no such file", Failed: true}}},
		{"claude failed result", `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Not logged in","num_turns":1,"duration_ms":1500}`,
			[]Event{{Kind: Result, Title: "Failed · Finished in 1.5s · 1 turns · $0.0000", Text: "Not logged in", Failed: true}}},
		{"codex failed command", `{"type":"item.completed","item":{"type":"command_execution","command":"go test","aggregated_output":"FAIL","exit_code":1,"status":"failed"}}`,
			[]Event{{Kind: Command, Title: "go test (exit 1)", Text: "FAIL", Failed: true}}},
		{"codex file change", `{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"a.go","kind":"add"},{"path":"b.go","kind":"update"}],"status":"completed"}}`,
			[]Event{{Kind: FileChange, Title: "add a.go, update b.go"}}},
		{"codex in-progress item", `{"type":"item.started","item":{"type":"command_execution","command":"ls"}}`, nil},
		{"codex todo list", `{"type":"item.completed","item":{"type":"todo_list","items":[{"text":"a","completed":true},{"text":"b","completed":false}]}}`,
			[]Event{{Kind: Info, Title: "Plan", Text: "[x] a\n[ ] b"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse([]byte(tt.line))
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("event %d:\n got %+v\nwant %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestReadFile(t *testing.T) {
	events, err := ReadFile(filepath.Join("testdata", "claude.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Kind != Result {
		t.Errorf("events = %+v, want them to end with the result", events)
	}

	// A line still being written is left out.
	path := filepath.Join(t.TempDir(), "turn.jsonl")
	data := `{"type":"item.completed","item":{"type":"agent_message","text":"one"}}` + "\n\n" +
		`{"type":"item.completed","item":{"type":"agent_message","text":"tw`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err = ReadFile(path)
	if err != nil || len(events) != 1 || events[0].Text != "one" {
		t.Errorf("ReadFile = %+v, %v; want only the complete message", events, err)
	}
	if _, err := ReadFile(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("ReadFile of a missing file succeeded")
	}
}
