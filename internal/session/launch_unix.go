//go:build unix

package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Launch starts the engine for folder: exe run with the arguments "engine"
// and folder, which must call Engine. The engine runs in a session of its
// own, so it keeps running when this process exits, and its output replaces
// the engine log. Save the session's settings before calling Launch.
func Launch(exe, folder string) (pid int, err error) {
	if err := os.MkdirAll(filepath.Join(folder, LocalDir), 0o755); err != nil {
		return 0, err
	}
	log, err := os.Create(engineLogPath(folder))
	if err != nil {
		return 0, err
	}
	defer log.Close()
	cmd := exec.Command(exe, "engine", folder)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go cmd.Wait() // reap the engine if it exits before this process does
	return cmd.Process.Pid, nil
}
