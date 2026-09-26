// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshutil

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wavetermdev/waveterm/pkg/baseds"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

// TestNoResponseSendSpuriousTimeout: every NoResponse request is registered in
// RpcMap under the same key "" and, once finalized, its timeout goroutine asks
// runServer to unregisterRpc(""), which cancels the context of whichever NoResponse
// request is registered under "" *now*. If that later request is waiting for room
// in OutputCh (the peer reads a little slower than we write, e.g. a burst of small
// stream packets), it fails immediately with "timeout sending request" although it
// waited far less than the 5s timeout. For the job manager that drops a stream
// packet, which leaves a permanent hole in the stream.
func TestNoResponseSendSpuriousTimeout(t *testing.T) {
	inputCh := make(chan baseds.RpcInputChType, DefaultInputChSize)
	outputCh := make(chan []byte, DefaultOutputChSize)
	rpc := MakeWshRpcWithChannels(inputCh, outputCh, wshrpc.RpcContext{}, nil, "race-test")
	// a slightly slow consumer (the socket writer), never stalled for long
	go func() {
		for range outputCh {
			time.Sleep(200 * time.Microsecond)
		}
	}()
	const total = 5000
	failures := 0
	var maxWaitFailed time.Duration
	var firstErr string
	for i := 0; i < total; i++ {
		start := time.Now()
		err := rpc.SendCommand(wshrpc.Command_StreamData, wshrpc.CommandStreamData{Id: "s", Seq: int64(i)}, &wshrpc.RpcOpts{NoResponse: true})
		if err != nil {
			failures++
			if w := time.Since(start); w > maxWaitFailed {
				maxWaitFailed = w
			}
			if firstErr == "" {
				firstErr = err.Error()
			}
		}
	}
	fmt.Printf("NoResponse sends: %d/%d failed (%q), longest wait of a failed send: %v (timeout is %dms)\n",
		failures, total, firstErr, maxWaitFailed, DefaultTimeoutMs)
	if failures > 0 && strings.Contains(firstErr, "timeout sending request") && maxWaitFailed < time.Second {
		t.Logf("reproduced spurious 'timeout sending request'")
	}
}
