//go:build !windows

package main

import (
	"bufio"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

var ( //nolint:gochecknoglobals // process-wide stderr fd filter
	stderrFilterOnce   sync.Once
	stderrPipeWriter   *os.File
	stderrFilterDone   chan struct{}
	stderrFilterActive bool
)

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
		stderrPipeWriter = writer
		stderrFilterDone = make(chan struct{})
		stderrFilterActive = true
		os.Stderr = os.NewFile(uintptr(unix.Stderr), "/dev/stderr")
		orig := os.NewFile(uintptr(origFD), "/dev/stderr-original")
		go func() {
			defer close(stderrFilterDone)
			copyFilteredStderr(reader, orig)
		}()
	})
}

// flushStderrFilter closes the pipe write ends so the filter goroutine
// sees EOF and drains any buffered output before the process exits.
func flushStderrFilter() {
	if !stderrFilterActive {
		return
	}
	_ = stderrPipeWriter.Close()
	_ = unix.Close(unix.Stderr)
	<-stderrFilterDone
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
