// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package logutil

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// NonBlockingWriter queues writes to an underlying writer from a background goroutine
// and drops them when the queue is full, so callers never block on a stalled sink.
//
// connserver logs to stderr, which is a pipe to sshd. When the ssh connection stalls
// (laptop asleep, VPN half-open, bad network), that pipe fills and every log.Printf
// blocks. Several of those calls run while holding the router lock, which wedges the
// whole connserver router: it stops reading every job manager socket, the job managers'
// sends time out and their streams wedge. Logging must never be able to do that.
type NonBlockingWriter struct {
	dest    io.Writer
	queue   chan []byte
	dropped atomic.Int64
	once    sync.Once
}

func NewNonBlockingWriter(dest io.Writer, queueLen int) *NonBlockingWriter {
	w := &NonBlockingWriter{dest: dest, queue: make(chan []byte, queueLen)}
	go w.run()
	return w
}

func (w *NonBlockingWriter) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case w.queue <- buf:
	default:
		w.dropped.Add(1)
	}
	return len(p), nil
}

func (w *NonBlockingWriter) Dropped() int64 {
	return w.dropped.Load()
}

func (w *NonBlockingWriter) run() {
	var reported int64
	for msg := range w.queue {
		if dropped := w.dropped.Load(); dropped > reported {
			fmt.Fprintf(w.dest, "[logutil] dropped %d log lines while output was blocked\n", dropped-reported)
			reported = dropped
		}
		w.dest.Write(msg)
	}
}
