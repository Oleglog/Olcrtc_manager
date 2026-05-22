//go:build !windows

package main

import (
	"bufio"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

var stderrFilterOnce sync.Once //nolint:gochecknoglobals // process-wide stderr fd filter

func installStderrFilter() {
	stderrFilterOnce.Do(func() {
		origFD, err := unix.Dup(int(os.Stderr.Fd()))
		if err != nil {
			return
		}
		reader, writer, err := os.Pipe()
		if err != nil {
			_ = unix.Close(origFD)
			return
		}
		if err := unix.Dup2(int(writer.Fd()), int(os.Stderr.Fd())); err != nil {
			_ = reader.Close()
			_ = writer.Close()
			_ = unix.Close(origFD)
			return
		}
		_ = writer.Close()
		os.Stderr = os.NewFile(uintptr(unix.Stderr), "/dev/stderr")
		orig := os.NewFile(uintptr(origFD), "/dev/stderr-original")
		go copyFilteredStderr(reader, orig)
	})
}

func copyFilteredStderr(reader *os.File, out io.Writer) {
	defer func() { _ = reader.Close() }()
	br := bufio.NewReader(reader)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && !isNoisyLogLine(line) {
			if _, writeErr := out.Write(line); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}
