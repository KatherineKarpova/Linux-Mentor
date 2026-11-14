package main

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type Terminal struct {
	mu   sync.Mutex
	ptmx *os.File
}

func (t *Terminal) Start(ctx context.Context, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "LINUX_MENTOR_EMBEDDED=1")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.ptmx = ptmx
	t.mu.Unlock()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				runtime.EventsEmit(ctx, "term:data", base64.StdEncoding.EncodeToString(buf[:n]))
			}
			if err != nil {
				runtime.EventsEmit(ctx, "term:exit", "")
				return
			}
		}
	}()
	return nil
}

func (t *Terminal) Write(data string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ptmx != nil {
		_, _ = t.ptmx.Write([]byte(data))
	}
}

func (t *Terminal) Resize(cols, rows int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ptmx != nil {
		_ = pty.Setsize(t.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	}
}