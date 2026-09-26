// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package jobmanager

import (
	"encoding/base64"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

type testWriter struct {
	mu      sync.Mutex
	packets []wshrpc.CommandStreamData
}

func (tw *testWriter) SendData(pkt wshrpc.CommandStreamData) error {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.packets = append(tw.packets, pkt)
	return nil
}

func (tw *testWriter) GetPackets() []wshrpc.CommandStreamData {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	result := make([]wshrpc.CommandStreamData, len(tw.packets))
	copy(result, tw.packets)
	return result
}

func (tw *testWriter) Clear() {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.packets = nil
}

func decodeData(data64 string) string {
	decoded, _ := base64.StdEncoding.DecodeString(data64)
	return string(decoded)
}

func TestBasicDisconnectedMode(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("hello world")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) > 0 {
		t.Errorf("Expected no packets in DISCONNECTED mode without client, got %d", len(packets))
	}

	sm.Close()
}

func TestConnectedModeBasicFlow(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("hello")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) == 0 {
		t.Fatal("Expected packets after ClientConnected")
	}

	// Verify we got the data
	allData := ""
	for _, pkt := range packets {
		if pkt.Data64 != "" {
			allData += decodeData(pkt.Data64)
		}
	}

	if allData != "hello" {
		t.Errorf("Expected 'hello', got '%s'", allData)
	}

	// Send ACK
	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "1", Seq: 5, RWnd: CwndSize})

	time.Sleep(50 * time.Millisecond)

	// Check for EOF packet
	packets = tw.GetPackets()
	hasEof := false
	for _, pkt := range packets {
		if pkt.Eof {
			hasEof = true
		}
	}

	if !hasEof {
		t.Error("Expected EOF packet after ACKing all data")
	}

	sm.Close()
}

func TestDisconnectedToConnectedTransition(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("test data")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) == 0 {
		t.Fatal("Expected cirbuf drain after connect")
	}

	allData := ""
	for _, pkt := range packets {
		if pkt.Data64 != "" {
			allData += decodeData(pkt.Data64)
		}
	}

	if allData != "test data" {
		t.Errorf("Expected 'test data', got '%s'", allData)
	}

	sm.Close()
}

func TestConnectedToDisconnectedTransition(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := &slowReader{data: []byte("slow data"), delay: 50 * time.Millisecond}
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	sm.ClientDisconnected()

	time.Sleep(100 * time.Millisecond)

	sm.Close()
}

func TestFlowControl(t *testing.T) {
	cwndSize := 1024
	tw := &testWriter{}
	sm := MakeStreamManagerWithSizes(cwndSize, 8*1024)

	largeData := strings.Repeat("x", cwndSize+500)
	reader := strings.NewReader(largeData)

	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, err = sm.ClientConnected("1", tw, cwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	totalData := 0
	for _, pkt := range packets {
		if pkt.Data64 != "" {
			decoded, _ := base64.StdEncoding.DecodeString(pkt.Data64)
			totalData += len(decoded)
		}
	}

	if totalData > cwndSize {
		t.Errorf("Sent %d bytes without ACK, exceeds cwnd size %d", totalData, cwndSize)
	}

	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "1", Seq: int64(totalData), RWnd: int64(cwndSize)})

	time.Sleep(100 * time.Millisecond)

	sm.Close()
}

func TestSequenceNumbering(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("abcdefghij")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) == 0 {
		t.Fatal("Expected packets")
	}

	expectedSeq := int64(0)
	for _, pkt := range packets {
		if pkt.Data64 == "" {
			continue
		}

		if pkt.Seq != expectedSeq {
			t.Errorf("Expected seq %d, got %d", expectedSeq, pkt.Seq)
		}

		decoded, _ := base64.StdEncoding.DecodeString(pkt.Data64)
		expectedSeq += int64(len(decoded))
	}

	sm.Close()
}

func TestTerminalEventOrdering(t *testing.T) {
	tw := &testWriter{}
	sm := MakeStreamManager()

	reader := strings.NewReader("data")
	err := sm.AttachReader(reader)
	if err != nil {
		t.Fatalf("AttachReader failed: %v", err)
	}

	_, err = sm.ClientConnected("1", tw, CwndSize, 0)
	if err != nil {
		t.Fatalf("ClientConnected failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	packets := tw.GetPackets()
	if len(packets) == 0 {
		t.Fatal("Expected data packets")
	}

	hasData := false
	hasEof := false
	eofSeq := int64(-1)

	for _, pkt := range packets {
		if pkt.Data64 != "" {
			hasData = true
		}
		if pkt.Eof {
			hasEof = true
			eofSeq = pkt.Seq
		}
	}

	if !hasData {
		t.Error("Expected data packet")
	}

	if hasEof {
		t.Error("Should not have EOF before ACK")
	}

	sm.RecvAck(wshrpc.CommandStreamAckData{Id: "1", Seq: 4, RWnd: CwndSize})

	time.Sleep(50 * time.Millisecond)

	packets = tw.GetPackets()
	hasEof = false
	for _, pkt := range packets {
		if pkt.Eof {
			hasEof = true
			eofSeq = pkt.Seq
		}
	}

	if !hasEof {
		t.Error("Expected EOF after ACKing all data")
	}

	if eofSeq != 4 {
		t.Errorf("Expected EOF at seq 4, got %d", eofSeq)
	}

	sm.Close()
}

type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

func (sr *slowReader) Read(p []byte) (n int, err error) {
	if sr.pos >= len(sr.data) {
		return 0, io.EOF
	}

	time.Sleep(sr.delay)

	n = copy(p, sr.data[sr.pos:])
	sr.pos += n

	return n, nil
}

// blackholeSender accepts packets but the client never ACKs (half-open / wedged client)
type blackholeSender struct {
	mu    sync.Mutex
	count int
	fail  error
}

func (bs *blackholeSender) SendData(pkt wshrpc.CommandStreamData) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.count++
	return bs.fail
}

// writeWithDeadline writes total bytes to w in chunks and fails if any chunk blocks
// longer than maxBlock (i.e. the "hosted program" is frozen on its tty).
func writeWithDeadline(t *testing.T, w io.Writer, total int, maxBlock time.Duration) {
	chunk := []byte(strings.Repeat("x", 1024))
	for written := 0; written < total; written += len(chunk) {
		done := make(chan struct{})
		go func() {
			w.Write(chunk)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(maxBlock):
			t.Fatalf("writer blocked for more than %v after %d bytes: hosted program would freeze", maxBlock, written)
		}
	}
}

func TestStalledClientIsDetachedAndReaderKeepsDraining(t *testing.T) {
	sm := MakeStreamManager()
	sm.SetStallTimeout(300 * time.Millisecond)
	brokenCh := make(chan string, 4)
	sm.SetClientBrokenHandler(func(sender DataSender, reason string) { brokenCh <- reason })
	sender := &blackholeSender{}
	if _, err := sm.ClientConnected("s1", sender, CwndSize, 0); err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	sm.AttachReader(pr)
	// 4 MB is far more than the 64 KB window and the 2 MB buffer
	writeWithDeadline(t, pw, 4*1024*1024, 2*time.Second)
	select {
	case reason := <-brokenCh:
		t.Logf("client detached: %s", reason)
	case <-time.After(2 * time.Second):
		t.Fatalf("broken-client handler not called")
	}
	if sm.BrokenDetachCount() != 1 {
		t.Fatalf("expected 1 detach, got %d", sm.BrokenDetachCount())
	}
	// a reconnecting client resumes; the head of the stream was dropped (gap), the tail is kept
	tw := &testWriter{}
	startSeq, err := sm.ClientConnected("s2", tw, CwndSize, 0)
	if err != nil {
		t.Fatal(err)
	}
	if startSeq != int64(4*1024*1024-CirBufSize) {
		t.Fatalf("expected resume at seq %d (last %d bytes kept), got %d", 4*1024*1024-CirBufSize, CirBufSize, startSeq)
	}
	pw.Close()
	sm.Close()
}

func TestAckingClientKeepsBackpressure(t *testing.T) {
	// a slow but alive client must keep lossless flow control (no detach)
	sm := MakeStreamManager()
	sm.SetStallTimeout(300 * time.Millisecond)
	tw := &testWriter{}
	if _, err := sm.ClientConnected("s1", tw, CwndSize, 0); err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	sm.AttachReader(pr)
	stop := make(chan struct{})
	go func() {
		// ack slowly: 16 KB every 100ms
		var acked int64
		for {
			select {
			case <-stop:
				return
			case <-time.After(100 * time.Millisecond):
			}
			var sent int64
			for _, p := range tw.GetPackets() {
				end := p.Seq + int64(len(decodeData(p.Data64)))
				if end > sent {
					sent = end
				}
			}
			next := acked + 16*1024
			if next > sent {
				next = sent
			}
			if next > acked {
				acked = next
				sm.RecvAck(wshrpc.CommandStreamAckData{Id: "s1", Seq: acked, RWnd: CwndSize})
			}
		}
	}()
	go func() {
		pw.Write([]byte(strings.Repeat("y", 512*1024)))
	}()
	time.Sleep(1500 * time.Millisecond)
	close(stop)
	if sm.BrokenDetachCount() != 0 {
		t.Fatalf("slow but acking client was detached")
	}
	pw.Close()
	sm.Close()
}

func TestSendErrorDetachesClient(t *testing.T) {
	sm := MakeStreamManager()
	brokenCh := make(chan string, 4)
	sm.SetClientBrokenHandler(func(sender DataSender, reason string) { brokenCh <- reason })
	sender := &blackholeSender{fail: io.ErrClosedPipe}
	if _, err := sm.ClientConnected("s1", sender, CwndSize, 0); err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	sm.AttachReader(pr)
	writeWithDeadline(t, pw, 1024*1024, 2*time.Second)
	select {
	case reason := <-brokenCh:
		t.Logf("client detached: %s", reason)
	case <-time.After(2 * time.Second):
		t.Fatalf("broken-client handler not called")
	}
	// nothing was acked, so a reconnect replays from 0 (no hole, no loss)
	tw := &testWriter{}
	startSeq, err := sm.ClientConnected("s2", tw, CwndSize, 0)
	if err != nil {
		t.Fatal(err)
	}
	if startSeq != 0 {
		t.Fatalf("expected replay from 0, got %d", startSeq)
	}
	pw.Close()
	sm.Close()
}
