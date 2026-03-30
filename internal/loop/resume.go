package loop

import (
	"fmt"
	"os"
	"strings"
)

// ResumeRound scans ralphDir for existing round files and returns the
// highest completed round number (0 if none found).
func ResumeRound(ralphDir, pattern string) int {
	entries, err := os.ReadDir(ralphDir)
	if err != nil {
		return 0
	}
	maxRound := 0
	suffixes := terminalSuffixes(pattern)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "round-") {
			continue
		}
		matched := false
		for _, suffix := range suffixes {
			if strings.HasSuffix(name, suffix) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		var n int
		fmt.Sscanf(name, "round-%d-", &n)
		if n > maxRound {
			maxRound = n
		}
	}
	return maxRound
}

func terminalSuffixes(pattern string) []string {
	switch pattern {
	case "standalone":
		return []string{"-standalone.json", "-builder.json"}
	case "think+act+evaluator":
		return []string{"-evaluator.json", "-tester.json"}
	case "think+act+evaluator+archivist":
		return []string{"-archivist.json", "-documenter.json"}
	default:
		return []string{"-executor.json", "-worker.json"}
	}
}
