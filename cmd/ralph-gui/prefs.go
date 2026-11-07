package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gioui.org/unit"
)

// prefs are the app's own settings, which belong to the user rather than
// to a project.
type prefs struct {
	SidebarWidth unit.Dp `json:"sidebar_width"`
	TurnsWidth   unit.Dp `json:"turns_width"`
	PlainText    bool    `json:"plain_text,omitempty"` // show Markdown as written
	Folder       string  `json:"folder,omitempty"`     // the last folder opened
}

func defaultPrefs() prefs { return prefs{SidebarWidth: 420, TurnsWidth: 230} }

func prefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ralph", "gui.json")
}

// loadPrefs returns the saved preferences, or the defaults.
func loadPrefs() prefs {
	p := defaultPrefs()
	if path := prefsPath(); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &p)
		}
	}
	return p
}

// save saves p. Preferences are a convenience, so failures are ignored.
func (p prefs) save() {
	path := prefsPath()
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		os.WriteFile(path, append(data, '\n'), 0o644)
	}
}
