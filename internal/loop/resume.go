package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"changkun.de/ralph/internal/backend"
)

// ResumeRound scans ralphDir for existing round files and returns the
// highest completed round number (0 if none found).
func ResumeRound(ralphDir string) int {
	entries, err := os.ReadDir(ralphDir)
	if err != nil {
		return 0
	}
	maxRound := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "round-") || !strings.HasSuffix(name, "-worker.json") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(name, "round-%d-worker.json", &n); err == nil && n > maxRound {
			maxRound = n
		}
	}
	return maxRound
}

// PreviousIdeas collects all thinker results from previous rounds for context.
func PreviousIdeas(ralphDir string, upTo int) []string {
	var ideas []string
	for i := 1; i <= upTo; i++ {
		data, err := os.ReadFile(filepath.Join(ralphDir, fmt.Sprintf("round-%03d-thinker.json", i)))
		if err != nil {
			continue
		}
		if idea := backend.ExtractResult(data); idea != "" {
			ideas = append(ideas, idea)
		}
	}
	sort.Strings(ideas)
	return ideas
}
