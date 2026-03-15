package loop

import (
	"fmt"
	"os"
	"strings"
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
		if !strings.HasPrefix(name, "round-") {
			continue
		}
		var n int
		switch {
		case strings.HasSuffix(name, "-worker.json"):
			fmt.Sscanf(name, "round-%d-worker.json", &n)
		case strings.HasSuffix(name, "-builder.json"):
			fmt.Sscanf(name, "round-%d-builder.json", &n)
		default:
			continue
		}
		if n > maxRound {
			maxRound = n
		}
	}
	return maxRound
}