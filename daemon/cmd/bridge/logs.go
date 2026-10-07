package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/phantranthelinh/browser-bridge/daemon/internal/home"
)

const followInterval = 250 * time.Millisecond

func logsCmd(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	follow := fs.Bool("f", false, "keep printing lines as the daemon writes them")
	n := fs.Int("n", 50, "number of lines")
	prev := fs.Bool("prev", false, "the log of the daemon's previous run")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *follow && *prev {
		fmt.Fprintln(stderr, "bridge logs: the previous run's log does not grow; drop -f or --prev")
		return 2
	}
	e, ok := loadHome(stderr)
	if !ok {
		return 1
	}
	path := home.LogPath(e.dir)
	if *prev {
		path += ".prev"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, "bridge logs:", err)
		return 1
	}
	lines := strings.SplitAfter(string(b), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	io.WriteString(stdout, strings.Join(lines[max(0, len(lines)-*n):], ""))
	if !*follow {
		return 0
	}
	return followLog(ctx, path, int64(len(b)), stdout)
}

// followLog prints what is appended to path from offset on, until ctx ends. A daemon restart
// moves the log to .prev and starts an empty one, which shows as the file getting shorter.
func followLog(ctx context.Context, path string, offset int64, stdout io.Writer) int {
	t := time.NewTicker(followInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-t.C:
		}
		f, err := os.Open(path)
		if err != nil {
			continue // between the rename and the new file
		}
		if st, err := f.Stat(); err == nil {
			if st.Size() < offset {
				offset = 0
			}
			if st.Size() > offset {
				f.Seek(offset, io.SeekStart)
				copied, _ := io.CopyN(stdout, f, st.Size()-offset)
				offset += copied
			}
		}
		f.Close()
	}
}
