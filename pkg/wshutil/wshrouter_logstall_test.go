// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshutil

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/wavetermdev/waveterm/pkg/baseds"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

// stallableWriter behaves like a pipe whose reader has gone away once stalled:
// every Write blocks. In connserver, log output goes to stderr, which is a pipe
// to sshd and therefore stalls whenever the ssh connection to the client stalls.
type stallableWriter struct {
	lock    sync.Mutex
	stalled bool
	cond    *sync.Cond
	w       io.Writer
}

func (sw *stallableWriter) Write(p []byte) (int, error) {
	sw.lock.Lock()
	for sw.stalled {
		sw.cond.Wait()
	}
	sw.lock.Unlock()
	return sw.w.Write(p)
}

func (sw *stallableWriter) setStalled(v bool) {
	sw.lock.Lock()
	sw.stalled = v
	sw.cond.Broadcast()
	sw.lock.Unlock()
}

// pushJobMessages feeds stream-data packets from a "job manager" link into the
// router (what connserver's job-socket reader goroutine does) and reports how many
// the router accepted before the reader goroutine would block.
func pushJobMessages(t *testing.T, stallLog bool) int {
	sw := &stallableWriter{w: os.Stderr}
	sw.cond = sync.NewCond(&sw.lock)
	log.SetOutput(sw)
	// order matters: un-stall first, a blocked log.Printf holds the logger's output lock
	defer func() {
		sw.setStalled(false)
		log.SetOutput(os.Stderr)
	}()

	router := NewWshRouter()
	// upstream (ssh to the laptop) is stalled: nobody drains ToRemoteCh
	upstream := MakeRpcProxy("upstream")
	router.RegisterUpstream(upstream)
	jobProxy := MakeRpcProxy("jobmanager")
	_, err := router.RegisterTrustedLeaf(jobProxy, MakeJobRouteId("test-job"))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	// from now on stderr (-> sshd) no longer drains
	sw.setStalled(stallLog)

	accepted := 0
	for i := 0; i < 2000; i++ {
		msg := RpcMessage{
			Command: wshrpc.Command_StreamData,
			Route:   "bare:laptop-reader",
			Data:    wshrpc.CommandStreamData{Id: "s1", Seq: int64(i * 10), Data64: "aGVsbG8K"},
		}
		barr, _ := json.Marshal(msg)
		select {
		case jobProxy.FromRemoteCh <- baseds.RpcInputChType{MsgBytes: barr}:
			accepted++
		case <-time.After(500 * time.Millisecond):
			return accepted
		}
	}
	return accepted
}

// TestRouterStallsWhenLogOutputBlocks shows that connserver's router stops
// reading *every* job-manager socket once its log output (stderr -> sshd) blocks:
// WshRouter.addToBacklog_withlock calls log.Printf while holding router.lock.
func TestRouterStallsWhenLogOutputBlocks(t *testing.T) {
	okCount := pushJobMessages(t, false)
	stalledCount := pushJobMessages(t, true)
	fmt.Printf("log output flowing: router accepted %d/2000 job messages (upstream stalled, backlog grows)\n", okCount)
	fmt.Printf("log output blocked: router accepted %d/2000 job messages, then the job socket reader blocks\n", stalledCount)
	if okCount != 2000 {
		t.Fatalf("expected router to backlog all messages when logging works, got %d", okCount)
	}
	if stalledCount >= 2000 {
		t.Fatalf("expected router to wedge when log output blocks")
	}
}
