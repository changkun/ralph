package session

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/changkun/ralph/internal/git"
)

// ErrNotRepo reports that a folder is not in a git repository.
var ErrNotRepo = errors.New("not a git repository; run git init in it first")

// The engine is a process that runs one session in the background, so that
// the session outlives the GUI that starts it. Launch starts it, and the
// GUI follows it through the status file, the engine log, and the round
// logs.

func engineLogPath(folder string) string { return filepath.Join(folder, LocalDir, "engine.log") }

// Engine runs a session on folder with its saved settings until the process
// is interrupted or terminated, printing progress to out. It is what the
// process Launch starts runs. Unlike Run, it does not make folder a git
// repository: the desktop app leaves that to the user.
func Engine(folder string, out io.Writer) error {
	if !git.IsRepo(folder) {
		return ErrNotRepo
	}
	s, err := Load(folder)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, folder, s, out)
}

// EngineLog returns the end of the engine log in folder, at most limit
// bytes, starting at a line boundary.
func EngineLog(folder string, limit int) string {
	f, err := os.Open(engineLogPath(folder))
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	start := max(0, fi.Size()-int64(limit))
	buf := make([]byte, fi.Size()-start)
	n, _ := f.ReadAt(buf, start)
	buf = buf[:n]
	if start > 0 {
		for i, b := range buf {
			if b == '\n' {
				buf = buf[i+1:]
				break
			}
		}
	}
	return string(buf)
}

// Alive reports whether a process with the given pid is running.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
