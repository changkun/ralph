package loop

import (
	"fmt"
	"os"
	"strings"

	"github.com/changkun/ralph/internal/backend"
)

// legacyRoleNames maps roles to the names their round logs had before the
// roles were renamed, so that older .ralph directories still resume.
var legacyRoleNames = map[backend.Role]string{
	backend.Standalone: "builder",
	backend.Executor:   "worker",
	backend.Evaluator:  "tester",
	backend.Archivist:  "documenter",
}

// ResumeRound returns the number of the last round of p that finished in an
// earlier run, or 0 if none did. A round counts as finished when ralphDir
// holds a log for the last role of p in that round.
func ResumeRound(ralphDir string, p Pattern) int {
	entries, err := os.ReadDir(ralphDir)
	if err != nil {
		return 0
	}
	role := p.lastRole()
	suffixes := []string{"-" + string(role) + ".json"}
	if legacy, ok := legacyRoleNames[role]; ok {
		suffixes = append(suffixes, "-"+legacy+".json")
	}
	last := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "round-") || !hasAnySuffix(name, suffixes) {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(name, "round-%d-", &n); err == nil && n > last {
			last = n
		}
	}
	return last
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}
