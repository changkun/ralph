package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParsePattern(t *testing.T) {
	tests := []struct {
		names []string
		want  Pattern
	}{
		{[]string{"standalone", "builder", "build-only", "build_only"}, Standalone},
		{[]string{"think+act", "think-act", "think_act", "strategist-executor"}, ThinkAct},
		{[]string{
			"think+act+evaluator", "think-act-evaluator", "think_act_evaluator",
			"strategist-executor-evaluator", "think+act+tester", "think-act-tester",
			"think_act_tester", "strategist-executor-tester",
		}, ThinkActEvaluator},
		{[]string{
			"think+act+evaluator+archivist", "think-act-evaluator-archivist",
			"think_act_evaluator_archivist", "strategist-executor-evaluator-archivist",
			"think+act+tester+documenter", "think-act-tester-documenter",
			"think_act_tester_documenter", "full-pipeline", "pipeline",
		}, ThinkActEvaluatorArchivist},
		{[]string{"thinkers"}, Thinkers},
	}
	for _, tt := range tests {
		for _, name := range tt.names {
			if got, err := ParsePattern(name); err != nil || got != tt.want {
				t.Errorf("ParsePattern(%q) = %q, %v; want %q", name, got, err, tt.want)
			}
		}
	}
	for _, name := range []string{"", "bad", "think+act+archivist"} {
		if _, err := ParsePattern(name); err == nil {
			t.Errorf("ParsePattern(%q) succeeded, want an error", name)
		}
	}
}

func touch(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResumeRound(t *testing.T) {
	tests := []struct {
		name    string
		pattern Pattern
		files   []string
		want    int
	}{
		{"empty", ThinkAct, nil, 0},
		{"think+act", ThinkAct, []string{"round-001-strategist.json", "round-001-executor.json", "round-002-strategist.json", "round-002-executor.json"}, 2},
		{"think+act unfinished round", ThinkAct, []string{"round-001-executor.json", "round-002-strategist.json"}, 1},
		{"think+act legacy", ThinkAct, []string{"round-001-worker.json", "round-002-worker.json"}, 2},
		{"standalone", Standalone, []string{"round-001-standalone.json", "round-002-standalone.json"}, 2},
		{"standalone legacy", Standalone, []string{"round-001-builder.json", "round-003-builder.json"}, 3},
		{"evaluator", ThinkActEvaluator, []string{"round-001-executor.json", "round-001-evaluator.json", "round-002-executor.json"}, 1},
		{"evaluator legacy", ThinkActEvaluator, []string{"round-001-tester.json", "round-002-tester.json"}, 2},
		{"archivist", ThinkActEvaluatorArchivist, []string{"round-001-evaluator.json", "round-001-archivist.json", "round-002-evaluator.json"}, 1},
		{"archivist legacy", ThinkActEvaluatorArchivist, []string{"round-001-tester.json", "round-001-documenter.json"}, 1},
		{"other files ignored", ThinkAct, []string{"notes-executor.json", "round-x-executor.json"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			touch(t, dir, tt.files...)
			if got := ResumeRound(dir, tt.pattern); got != tt.want {
				t.Errorf("ResumeRound = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResumeRoundMissingDir(t *testing.T) {
	if got := ResumeRound(filepath.Join(t.TempDir(), "missing"), ThinkAct); got != 0 {
		t.Errorf("ResumeRound = %d, want 0", got)
	}
}

func TestResumeRoundManyRounds(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 120; i++ {
		touch(t, dir, fmt.Sprintf("round-%03d-executor.json", i))
	}
	if got := ResumeRound(dir, ThinkAct); got != 120 {
		t.Errorf("ResumeRound = %d, want 120", got)
	}
}
