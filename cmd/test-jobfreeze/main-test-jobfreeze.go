// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// test-jobfreeze reproduces the "durable session freezes the hosted program" bug.
//
// It plays the role of wavesrv+connserver against a real `wsh jobmanager` process:
// it starts a job manager under a private HOME and a private job socket dir
// (WAVETERM_JOBSOCKETDIR), starts a probe program in the job's pty, consumes the
// output stream like wavesrv does, then injects a fault on the job socket and
// watches whether the hosted program keeps running. Finally it reattaches the way
// wavesrv does (PrepareConnect + StartStream) and checks that output resumes.
//
// Faults:
//
//	none      control run, the client keeps consuming
//	blackhole the client keeps reading the socket but drops all stream data and
//	          never acks (connserver alive and buffering, laptop gone / half-open)
//	stopread  the client stops reading the socket entirely (connserver's router
//	          wedged; produces "SendData: ... timeout sending request")
//	hiccup    like stopread, but only for -hiccup seconds, then reads resume
//	close     the client closes the socket (connserver exited, clean disconnect)
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/wavetermdev/waveterm/pkg/baseds"
	"github.com/wavetermdev/waveterm/pkg/streamclient"
	"github.com/wavetermdev/waveterm/pkg/wavejwt"
	"github.com/wavetermdev/waveterm/pkg/waveobj"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshrpc/wshclient"
	"github.com/wavetermdev/waveterm/pkg/wshutil"
)

const probeJs = `
const fs = require('fs');
const hb = process.argv[2];
const perTick = parseInt(process.argv[3] || '40', 10);
const tickMs = parseInt(process.argv[4] || '100', 10);
const padLen = parseInt(process.argv[5] || '100', 10);
let n = 0;
const pad = 'x'.repeat(padLen);
setInterval(() => {
  for (let i = 0; i < perTick; i++) {
    process.stdout.write(Date.now() + ' ' + (n++) + ' ' + pad + '\n');
  }
  fs.writeFileSync(hb, Date.now() + ' ' + n + '\n');
}, tickMs);
`

var (
	flagWsh      = flag.String("wsh", "", "path to wsh binary to test")
	flagDir      = flag.String("dir", "", "private test dir (HOME, sockets, logs live here)")
	flagFault    = flag.String("fault", "blackhole", "none|blackhole|stopread|hiccup|close")
	flagPre      = flag.Int("pre", 4, "seconds of normal streaming before the fault")
	flagFaultDur = flag.Int("faultsecs", 20, "seconds to observe while the fault is active")
	flagHiccup   = flag.Int("hiccup", 7, "for fault=hiccup: seconds the socket is not read")
	flagPost     = flag.Int("post", 6, "seconds to observe after reattach")
	flagPerTick  = flag.Int("pertick", 40, "probe lines per tick")
	flagTickMs   = flag.Int("tickms", 100, "probe tick interval (ms)")
	flagPad      = flag.Int("pad", 100, "probe line padding (bytes)")
	flagNoReatt  = flag.Bool("noreattach", false, "skip the reattach phase")
	flagSlowRead = flag.Int("slowreadus", 0, "client processes each incoming message this many microseconds slower (a busy connserver); no fault needed to trigger the spurious send timeout")
	flagTmux     = flag.Bool("tmux", false, "run the probe inside tmux (private -S socket in -dir); the job's pty hosts the tmux client")
)

// ---- a harness "main server" impl (what wavesrv/connserver answer to the job manager)

type harnessImpl struct{}

func (*harnessImpl) WshServerImpl() {}

func (*harnessImpl) AuthenticateJobManagerCommand(ctx context.Context, data wshrpc.CommandAuthenticateJobManagerData) error {
	return nil
}

func (*harnessImpl) JobCmdExitedCommand(ctx context.Context, data wshrpc.CommandJobCmdExitedData) error {
	logf("job cmd exited: %+v", data)
	return nil
}

// ---- a controllable job-socket client

type clientConn struct {
	conn      net.Conn
	rpc       *wshutil.WshRpc
	paused    atomic.Bool // stop reading the socket
	blackhole atomic.Bool // read, but drop stream data
	closed    atomic.Bool
	pauseCond *sync.Cond
	pauseLock sync.Mutex
}

type gatedReader struct {
	cc *clientConn
	r  io.Reader
}

func (g *gatedReader) Read(p []byte) (int, error) {
	g.cc.pauseLock.Lock()
	for g.cc.paused.Load() {
		g.cc.pauseCond.Wait()
	}
	g.cc.pauseLock.Unlock()
	return g.r.Read(p)
}

func (cc *clientConn) setPaused(v bool) {
	cc.pauseLock.Lock()
	cc.paused.Store(v)
	cc.pauseCond.Broadcast()
	cc.pauseLock.Unlock()
}

func dialJob(sockPath string) (*clientConn, error) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, err
	}
	cc := &clientConn{conn: conn}
	cc.pauseCond = sync.NewCond(&cc.pauseLock)
	inputCh := make(chan baseds.RpcInputChType, wshutil.DefaultInputChSize)
	outputCh := make(chan []byte, wshutil.DefaultOutputChSize)
	cc.rpc = wshutil.MakeWshRpcWithChannels(inputCh, outputCh, wshrpc.RpcContext{}, &harnessImpl{}, "harness")
	go func() {
		wshutil.AdaptOutputChToStream(outputCh, conn)
	}()
	go func() {
		defer close(inputCh)
		scanner := bufio.NewScanner(&gatedReader{cc: cc, r: conn})
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if cc.blackhole.Load() && bytes.Contains(line, []byte(`"command":"streamdata"`)) {
				continue
			}
			if *flagSlowRead > 0 {
				time.Sleep(time.Duration(*flagSlowRead) * time.Microsecond)
			}
			msg := make([]byte, len(line))
			copy(msg, line)
			inputCh <- baseds.RpcInputChType{MsgBytes: msg}
		}
	}()
	return cc, nil
}

func (cc *clientConn) close() {
	if cc.closed.Swap(true) {
		return
	}
	cc.setPaused(false)
	cc.conn.Close()
}

// ---- output consumer (like jobcontroller.runOutputLoop)

type consumer struct {
	total     atomic.Int64 // total stream position consumed (incl. gaps)
	lastLine  atomic.Value // last complete probe line counter seen
	gapBytes  atomic.Int64
	lineGaps  atomic.Int64
	prevCount int64
	partial   []byte
}

func (c *consumer) run(reader *streamclient.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			c.total.Add(int64(n))
			c.scanLines(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (c *consumer) scanLines(data []byte) {
	c.partial = append(c.partial, data...)
	for {
		idx := bytes.IndexByte(c.partial, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(c.partial[:idx]), "\r")
		c.partial = c.partial[idx+1:]
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		cnt, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		if c.prevCount > 0 && cnt != c.prevCount+1 {
			c.lineGaps.Add(cnt - c.prevCount - 1)
		}
		c.prevCount = cnt
		c.lastLine.Store(cnt)
	}
}

// ---- helpers

var startTs = time.Now()

func logf(format string, args ...any) {
	fmt.Printf("[%6.2fs] %s\n", time.Since(startTs).Seconds(), fmt.Sprintf(format, args...))
}

func readFileTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(b))
}

func procState(pid int) string {
	stat := readFileTrim(fmt.Sprintf("/proc/%d/stat", pid))
	state := "?"
	if idx := strings.LastIndex(stat, ")"); idx >= 0 && len(stat) > idx+2 {
		state = stat[idx+2 : idx+3]
	}
	return fmt.Sprintf("state=%s wchan=%s", state, readFileTrim(fmt.Sprintf("/proc/%d/wchan", pid)))
}

func hbInfo(hbPath string) (ageMs int64, count int64) {
	s := readFileTrim(hbPath)
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return -1, -1
	}
	ts, _ := strconv.ParseInt(fields[0], 10, 64)
	cnt, _ := strconv.ParseInt(fields[1], 10, 64)
	return time.Now().UnixMilli() - ts, cnt
}

type sampler struct {
	hbPath   string
	probePid int
	cons     *consumer
}

func (s *sampler) sample(label string) {
	age, cnt := hbInfo(s.hbPath)
	last, _ := s.cons.lastLine.Load().(int64)
	status := "RUNNING"
	if age > 1000+int64(*flagTickMs) {
		status = "FROZEN "
	}
	logf("%-9s probe %s hb_age=%5dms lines_written=%6d | client consumed=%8d bytes, last_line=%6d, line_gaps=%d | probe %s",
		label, status, age, cnt, s.cons.total.Load(), last, s.cons.lineGaps.Load(), procState(s.probePid))
}

func mustf(err error, what string) {
	if err != nil {
		log.Fatalf("%s: %v", what, err)
	}
}

func main() {
	flag.Parse()
	go func() {
		// watchdog so a wedged harness never hangs a script
		time.Sleep(time.Duration(*flagPre+*flagFaultDur+*flagPost+30) * time.Second)
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		fmt.Fprintf(os.Stderr, "harness watchdog fired, goroutines:\n%s\n", buf[:n])
		os.Exit(3)
	}()
	if *flagWsh == "" || *flagDir == "" {
		log.Fatalf("-wsh and -dir are required")
	}
	dir, _ := filepath.Abs(*flagDir)
	homeDir := filepath.Join(dir, "home")
	sockDir := filepath.Join(dir, "sock")
	for _, d := range []string{homeDir, sockDir} {
		mustf(os.MkdirAll(d, 0700), "mkdir")
	}
	hbPath := filepath.Join(dir, "probe.hb")
	probePath := filepath.Join(dir, "probe.js")
	mustf(os.WriteFile(probePath, []byte(probeJs), 0644), "write probe")
	nodePath, err := exec.LookPath("node")
	mustf(err, "node not found")

	kp, err := wavejwt.GenerateKeyPair()
	mustf(err, "keypair")
	mustf(wavejwt.SetPrivateKey(kp.PrivateKey), "setprivkey")
	mustf(wavejwt.SetPublicKey(kp.PublicKey), "setpubkey")
	jobId := uuid.New().String()
	clientId := uuid.New().String()
	jobAuthToken := strings.ReplaceAll(uuid.New().String(), "-", "")
	accessToken, err := wavejwt.Sign(&wavejwt.WaveJwtClaims{MainServer: true, JobId: jobId})
	mustf(err, "sign")

	// ---- start the job manager exactly like wshremote.RemoteStartJobCommand does
	readyR, readyW, err := os.Pipe()
	mustf(err, "pipe")
	jmCmd := exec.Command(*flagWsh, "jobmanager", "--jobid", jobId, "--clientid", clientId)
	jmCmd.Env = []string{
		"HOME=" + homeDir,
		"PATH=" + os.Getenv("PATH"),
		"WAVETERM_JOBSOCKETDIR=" + sockDir,
		"WAVETERM_PUBLICKEY=" + base64.StdEncoding.EncodeToString(kp.PublicKey),
	}
	jmCmd.ExtraFiles = []*os.File{readyW}
	stdin, err := jmCmd.StdinPipe()
	mustf(err, "stdin")
	mustf(jmCmd.Start(), "start jobmanager")
	readyW.Close()
	fmt.Fprintf(stdin, "Wave-JobAccessToken:%s\n", jobAuthToken)
	stdin.Close()
	readyLine, _ := bufio.NewReader(readyR).ReadString('\n')
	if !strings.Contains(readyLine, "Wave-JobManagerStart") {
		log.Fatalf("job manager did not signal ready: %q", readyLine)
	}
	go jmCmd.Wait()
	jmPid := jmCmd.Process.Pid
	jobLog := filepath.Join(homeDir, ".waveterm", "jobs", jobId+".log")
	sockPath := filepath.Join(sockDir, jobId+".sock")
	logf("job manager pid=%d jobid=%s log=%s", jmPid, jobId, jobLog)

	var probePid int
	cleanup := func() {
		if probePid > 0 {
			syscall.Kill(probePid, syscall.SIGKILL)
		}
		syscall.Kill(jmPid, syscall.SIGKILL)
	}
	defer cleanup()

	// ---- connect + start job (StartJob path)
	cc, err := dialJob(sockPath)
	mustf(err, "dial")
	mustf(wshclient.AuthenticateToJobManagerCommand(cc.rpc, wshrpc.CommandAuthenticateToJobData{JobAccessToken: accessToken}, nil), "authenticate")
	readerRoute := "bare:harness"
	writerRoute := wshutil.MakeJobRouteId(jobId)
	reader, streamMeta := cc.rpc.StreamBroker.CreateStreamReader(readerRoute, writerRoute, 64*1024)
	cons := &consumer{}
	go cons.run(reader)
	jobCmd := nodePath
	jobArgs := []string{probePath, hbPath, strconv.Itoa(*flagPerTick), strconv.Itoa(*flagTickMs), strconv.Itoa(*flagPad)}
	tmuxSock := filepath.Join(dir, "tmux.sock")
	if *flagTmux {
		tmuxPath, err := exec.LookPath("tmux")
		mustf(err, "tmux not found")
		jobCmd = tmuxPath
		probeCmdline := nodePath + " " + strings.Join(jobArgs, " ")
		jobArgs = []string{"-S", tmuxSock, "-f", "/dev/null", "new-session", "-A", "-s", "probe", probeCmdline}
		defer exec.Command(tmuxPath, "-S", tmuxSock, "kill-server").Run()
	}
	rtn, err := wshclient.StartJobCommand(cc.rpc, wshrpc.CommandStartJobData{
		Cmd:        jobCmd,
		Args:       jobArgs,
		Env:        map[string]string{"PATH": os.Getenv("PATH"), "HOME": homeDir, "TERM": "xterm-256color"},
		TermSize:   waveobj.TermSize{Rows: 40, Cols: 160},
		StreamMeta: streamMeta,
	}, &wshrpc.RpcOpts{Timeout: 10000})
	mustf(err, "startjob")
	probePid = rtn.CmdPid
	if *flagTmux {
		// the job's direct child is the tmux client; find the node probe inside the tmux server
		for i := 0; i < 50; i++ {
			out, _ := exec.Command("tmux", "-S", tmuxSock, "list-panes", "-a", "-F", "#{pane_pid}").Output()
			if pid, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && pid > 0 {
				logf("job pid=%d is the tmux client; probe (tmux pane) pid=%d", probePid, pid)
				probePid = pid
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	logf("probe started pid=%d (node, writes ~%d bytes/s to its tty in %d-byte lines)", probePid, *flagPerTick*(1000 / *flagTickMs)*(*flagPad+20), *flagPad+20)
	smp := &sampler{hbPath: hbPath, probePid: probePid, cons: cons}

	for i := 0; i < *flagPre; i++ {
		time.Sleep(time.Second)
		smp.sample("normal")
	}

	// ---- inject the fault
	logf("=== FAULT: %s ===", *flagFault)
	switch *flagFault {
	case "none":
	case "blackhole":
		cc.blackhole.Store(true)
	case "stopread", "hiccup":
		cc.setPaused(true)
	case "close":
		cc.close()
	default:
		log.Fatalf("unknown fault %q", *flagFault)
	}
	for i := 0; i < *flagFaultDur; i++ {
		time.Sleep(time.Second)
		if *flagFault == "hiccup" && i+1 == *flagHiccup {
			logf("=== hiccup over: client reads the socket again ===")
			cc.setPaused(false)
		}
		smp.sample("fault")
	}
	logf("--- job manager log (fault phase) ---")
	printLogTail(jobLog, 12)

	if *flagNoReatt {
		return
	}

	// ---- reattach like wavesrv's restartStreaming (new connserver connection)
	logf("=== REATTACH (new connection, PrepareConnect seq=%d) ===", cons.total.Load())
	reader.Close()
	cc2, err := dialJob(sockPath)
	mustf(err, "dial2")
	mustf(wshclient.AuthenticateToJobManagerCommand(cc2.rpc, wshrpc.CommandAuthenticateToJobData{JobAccessToken: accessToken}, nil), "authenticate2")
	cc.close()
	curSeq := cons.total.Load()
	reader2, streamMeta2 := cc2.rpc.StreamBroker.CreateStreamReaderWithSeq(readerRoute, writerRoute, 64*1024, curSeq)
	prep, err := wshclient.JobPrepareConnectCommand(cc2.rpc, wshrpc.CommandJobPrepareConnectData{
		StreamMeta: *streamMeta2,
		Seq:        curSeq,
		TermSize:   waveobj.TermSize{Rows: 40, Cols: 160},
	}, &wshrpc.RpcOpts{Timeout: 5000})
	mustf(err, "prepareconnect")
	if prep.Seq > curSeq {
		logf("server seq %d > client seq %d: gap of %d bytes (output dropped while detached)", prep.Seq, curSeq, prep.Seq-curSeq)
		cons.gapBytes.Store(prep.Seq - curSeq)
		cons.total.Store(prep.Seq)
		reader2.UpdateNextSeq(prep.Seq)
		cons.prevCount = 0
		cons.partial = nil
	}
	mustf(wshclient.JobStartStreamCommand(cc2.rpc, wshrpc.CommandJobStartStreamData{}, &wshrpc.RpcOpts{Timeout: 5000}), "startstream")
	go cons.run(reader2)
	for i := 0; i < *flagPost; i++ {
		time.Sleep(time.Second)
		smp.sample("reattach")
	}
	logf("--- job manager log (reattach) ---")
	printLogTail(jobLog, 8)
}

func printLogTail(path string, n int) {
	b, err := os.ReadFile(path)
	if err != nil {
		logf("cannot read job log: %v", err)
		return
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	// collapse repeats so floods stay readable
	type entry struct {
		key   string
		text  string
		count int
	}
	var ents []entry
	for _, l := range lines {
		txt := l
		if len(l) > 20 {
			txt = l[20:]
		}
		if len(ents) > 0 && ents[len(ents)-1].key == txt {
			ents[len(ents)-1].count++
			continue
		}
		ents = append(ents, entry{key: txt, text: l, count: 1})
	}
	if len(ents) > n {
		ents = ents[len(ents)-n:]
	}
	for _, e := range ents {
		if e.count > 1 {
			fmt.Printf("    %s   (x%d)\n", e.text, e.count)
		} else {
			fmt.Printf("    %s\n", e.text)
		}
	}
}
