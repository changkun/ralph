package loop

import (
	"fmt"
	"slices"
	"strings"

	"github.com/changkun/ralph/internal/backend"
)

// Pattern selects which agent roles take part in each round. The patterns
// are those described in https://changkun.de/blog/posts/goalless-agents/.
type Pattern string

// Supported patterns, from fewest roles to most.
const (
	Standalone                 Pattern = "standalone"
	ThinkAct                   Pattern = "think+act"
	ThinkActEvaluator          Pattern = "think+act+evaluator"
	ThinkActEvaluatorArchivist Pattern = "think+act+evaluator+archivist"
)

// Patterns lists the supported patterns, from fewest roles to most.
var Patterns = []Pattern{Standalone, ThinkAct, ThinkActEvaluator, ThinkActEvaluatorArchivist}

// patternNames maps every accepted pattern name, with "+" as the only
// separator, to its pattern. Besides the canonical names it holds the
// legacy names from before roles were renamed.
var patternNames = map[string]Pattern{
	"standalone": Standalone,
	"builder":    Standalone,
	"build+only": Standalone,

	"think+act":           ThinkAct,
	"strategist+executor": ThinkAct,

	"think+act+evaluator":           ThinkActEvaluator,
	"think+act+tester":              ThinkActEvaluator,
	"strategist+executor+evaluator": ThinkActEvaluator,
	"strategist+executor+tester":    ThinkActEvaluator,

	"think+act+evaluator+archivist":           ThinkActEvaluatorArchivist,
	"think+act+tester+documenter":             ThinkActEvaluatorArchivist,
	"strategist+executor+evaluator+archivist": ThinkActEvaluatorArchivist,
	"pipeline":      ThinkActEvaluatorArchivist,
	"full+pipeline": ThinkActEvaluatorArchivist,
}

// ParsePattern returns the pattern called name. It accepts the canonical
// names, the legacy names, and either "-" or "_" in place of "+".
func ParsePattern(name string) (Pattern, error) {
	key := strings.NewReplacer("-", "+", "_", "+").Replace(name)
	if p, ok := patternNames[key]; ok {
		return p, nil
	}
	names := make([]string, len(Patterns))
	for i, p := range Patterns {
		names[i] = string(p)
	}
	return "", fmt.Errorf("unknown pattern %q; want one of %s", name, strings.Join(names, ", "))
}

// roles returns the roles that run in each round of p, in order.
func (p Pattern) roles() []backend.Role {
	switch p {
	case Standalone:
		return []backend.Role{backend.Standalone}
	case ThinkActEvaluator:
		return []backend.Role{backend.Strategist, backend.Executor, backend.Evaluator}
	case ThinkActEvaluatorArchivist:
		return []backend.Role{backend.Strategist, backend.Executor, backend.Evaluator, backend.Archivist}
	default:
		return []backend.Role{backend.Strategist, backend.Executor}
	}
}

func (p Pattern) has(role backend.Role) bool { return slices.Contains(p.roles(), role) }

// lastRole returns the role whose round log marks a round of p as done.
func (p Pattern) lastRole() backend.Role {
	roles := p.roles()
	return roles[len(roles)-1]
}
