package grbl

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCommandReceivesOK(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if strings.HasSuffix(string(data), "\n") {
			p.enqueueLine("ok")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	if err := sender.Command(context.Background(), "G1 X1"); err != nil {
		t.Fatalf("Command: %v", err)
	}
}

func TestCommandReceivesError(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if strings.HasSuffix(string(data), "\n") {
			p.enqueueLine("error:20")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	err := sender.Command(context.Background(), "G1 X1")
	if err == nil || err.Error() != "error:20" {
		t.Fatalf("Command error = %v, want error:20", err)
	}
}

func TestCommandReceivesALARM(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if strings.HasSuffix(string(data), "\n") {
			p.enqueueLine("ALARM:1")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	err := sender.Command(context.Background(), "G1 X1")
	if err == nil || err.Error() != "ALARM:1" {
		t.Fatalf("Command error = %v, want ALARM:1", err)
	}
}

func TestStatusReceivesReport(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if string(data) == "?" {
			p.enqueueLine("<Idle|MPos:0.000,0.000,0.000>")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	got, err := sender.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got != "<Idle|MPos:0.000,0.000,0.000>" {
		t.Fatalf("Status = %q", got)
	}
}

func TestWaitForStateRecognizesIdle(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if string(data) == "?" {
			p.enqueueLine("<Idle|MPos:0.000,0.000,0.000>")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	state, _, err := sender.WaitForState(context.Background(), "Idle")
	if err != nil {
		t.Fatalf("WaitForState: %v", err)
	}
	if state != "Idle" {
		t.Fatalf("state = %q, want Idle", state)
	}
}

func TestWaitForStateRecognizesHoldForms(t *testing.T) {
	for _, report := range []string{
		"<Hold|MPos:0.000,0.000,0.000>",
		"<Hold:0|MPos:0.000,0.000,0.000>",
	} {
		t.Run(report, func(t *testing.T) {
			port := newFakePort()
			port.setOnWrite(func(p *fakePort, data []byte) {
				if string(data) == "?" {
					p.enqueueLine(report)
				}
			})
			sender := newTestSender(port)
			defer sender.Close()

			state, _, err := sender.WaitForState(context.Background(), "Hold")
			if err != nil {
				t.Fatalf("WaitForState: %v", err)
			}
			if stateBase(state) != "Hold" {
				t.Fatalf("state = %q, want Hold base", state)
			}
		})
	}
}

func TestStatusStateParsing(t *testing.T) {
	tests := map[string]string{
		"<Idle|MPos:0.000,0.000,0.000>": "Idle",
		"<Run,MPos:0.000,0.000,0.000>":  "Run",
		"<Hold:0|MPos:0,0,0>":           "Hold:0",
	}
	for report, want := range tests {
		if got := StatusState(report); got != want {
			t.Fatalf("StatusState(%q) = %q, want %q", report, got, want)
		}
	}
}

func TestAsyncInformationalMessageBeforeOK(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if strings.HasSuffix(string(data), "\n") {
			p.enqueueLine("[MSG:hello]")
			p.enqueueLine("ok")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	if err := sender.Command(context.Background(), "G1 X1"); err != nil {
		t.Fatalf("Command: %v", err)
	}
	assertEvent(t, sender, "[MSG:hello]")
}

func TestStatusReportBeforeCommandOK(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if strings.HasSuffix(string(data), "\n") {
			p.enqueueLine("<Run|MPos:1.000,0.000,0.000>")
			p.enqueueLine("ok")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	if err := sender.Command(context.Background(), "G1 X1"); err != nil {
		t.Fatalf("Command: %v", err)
	}
	assertEvent(t, sender, "<Run|MPos:1.000,0.000,0.000>")
}

func TestOKWhileStatusWaiterActive(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if string(data) == "?" {
			p.enqueueLine("ok")
			p.enqueueLine("<Idle|MPos:0.000,0.000,0.000>")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	report, err := sender.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if StatusState(report) != "Idle" {
		t.Fatalf("report = %q", report)
	}
	assertEvent(t, sender, "ok")
}

func TestCommandTimeoutDesynchronizes(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)
	defer sender.Close()

	err := sender.Command(context.Background(), "G1 X1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Command error = %v, want deadline", err)
	}
	err = sender.Command(context.Background(), "G1 X2")
	if !errors.Is(err, ErrDesynchronized) {
		t.Fatalf("next Command error = %v, want ErrDesynchronized", err)
	}
}

func TestCommandCancellationDesynchronizes(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)
	defer sender.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- sender.Command(ctx, "G1 X1")
	}()
	port.waitWrite(t, "G1 X1\n")
	cancel()

	err := <-errc
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Command error = %v, want canceled", err)
	}
	err = sender.Command(context.Background(), "G1 X2")
	if !errors.Is(err, ErrDesynchronized) {
		t.Fatalf("next Command error = %v, want ErrDesynchronized", err)
	}
}

func TestLaterCommandAfterTimeoutDoesNotConsumeStaleResponse(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)
	defer sender.Close()

	err := sender.Command(context.Background(), "G1 X1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Command error = %v, want deadline", err)
	}
	port.enqueueLine("ok")

	err = sender.Command(context.Background(), "G1 X2")
	if !errors.Is(err, ErrDesynchronized) {
		t.Fatalf("next Command error = %v, want ErrDesynchronized", err)
	}
	if port.countWrite("G1 X2\n") != 0 {
		t.Fatal("desynchronized command was written")
	}
}

func TestSoftResetRestoresSynchronization(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)
	defer sender.Close()

	err := sender.Command(context.Background(), "G1 X1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Command error = %v, want deadline", err)
	}
	port.enqueueLine("ok")
	port.setOnWrite(func(p *fakePort, data []byte) {
		switch string(data) {
		case string([]byte{0x18}):
			p.enqueueLine("Grbl 1.1h ['$' for help]")
		default:
			if strings.HasSuffix(string(data), "\n") {
				p.enqueueLine("ok")
			}
		}
	})

	if err := sender.SoftReset(context.Background()); err != nil {
		t.Fatalf("SoftReset: %v", err)
	}
	if err := sender.Command(context.Background(), "G1 X2"); err != nil {
		t.Fatalf("Command after reset: %v", err)
	}
}

func TestStartupMessageDetectionAfterSoftReset(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if string(data) == string([]byte{0x18}) {
			p.enqueueLine("[MSG:reset]")
			p.enqueueLine("Grbl 1.1h ['$' for help]")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	if err := sender.SoftReset(context.Background()); err != nil {
		t.Fatalf("SoftReset: %v", err)
	}
	assertEvent(t, sender, "[MSG:reset]")
}

func TestPortClosureWhileCommandWaiting(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)

	errc := make(chan error, 1)
	go func() {
		errc <- sender.Command(context.Background(), "G1 X1")
	}()
	port.waitWrite(t, "G1 X1\n")
	if err := sender.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := <-errc; err == nil {
		t.Fatal("Command succeeded after port closure")
	}
}

func TestPortClosureWhileStatusPolling(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)

	errc := make(chan error, 1)
	go func() {
		_, err := sender.Status(context.Background())
		errc <- err
	}()
	port.waitWrite(t, "?")
	if err := sender.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := <-errc; err == nil {
		t.Fatal("Status succeeded after port closure")
	}
}

func TestReaderFailurePropagation(t *testing.T) {
	readErr := errors.New("serial read failed")
	port := newFakePort()
	sender := newTestSender(port)
	defer sender.Close()

	port.injectError(readErr)
	err := sender.Command(context.Background(), "G1 X1")
	if !errors.Is(err, readErr) {
		t.Fatalf("Command error = %v, want read error", err)
	}

	_, err = sender.Status(context.Background())
	if !errors.Is(err, readErr) {
		t.Fatalf("Status error = %v, want read error", err)
	}
}

func TestRepeatedClose(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)

	if err := sender.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := sender.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestConcurrentClose(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- sender.Close()
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("Close error = %v", err)
		}
	}
}

func TestNoDuplicateReaderLoops(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)
	defer sender.Close()

	port.waitRead(t)
	if got := port.readCallCount(); got != 1 {
		t.Fatalf("initial read calls = %d, want 1", got)
	}
}

func TestNoConcurrentReadsFromPort(t *testing.T) {
	port := newFakePort()
	port.setOnWrite(func(p *fakePort, data []byte) {
		if string(data) == "?" {
			p.enqueueLine("<Idle|MPos:0.000,0.000,0.000>")
		}
	})
	sender := newTestSender(port)
	defer sender.Close()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = sender.Status(context.Background())
		}()
	}
	wg.Wait()

	if got := port.maxActiveReadCount(); got > 1 {
		t.Fatalf("max concurrent reads = %d, want <= 1", got)
	}
}

func TestFeedHoldDuringBlockedCommandWait(t *testing.T) {
	port := newFakePort()
	sender := newTestSender(port)
	defer sender.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		errc <- sender.Command(ctx, "G1 X1")
	}()
	port.waitWrite(t, "G1 X1\n")

	if err := sender.FeedHold(); err != nil {
		t.Fatalf("FeedHold: %v", err)
	}
	port.waitWrite(t, "!")

	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("Command error = %v, want canceled", err)
	}
}

func newTestSender(port *fakePort) *Sender {
	return New(port, Options{
		CommandTimeout: 20 * time.Millisecond,
		IdleTimeout:    20 * time.Millisecond,
		PollInterval:   time.Millisecond,
		Log:            io.Discard,
	})
}

func assertEvent(t *testing.T, sender *Sender, want string) {
	t.Helper()
	for _, got := range sender.EventHistory() {
		if got == want {
			return
		}
	}
	t.Fatalf("event %q not found in %v", want, sender.EventHistory())
}

type fakeRead struct {
	b   byte
	err error
}

type fakePort struct {
	mu             sync.Mutex
	closeOnce      sync.Once
	reads          chan fakeRead
	closed         chan struct{}
	readObserved   chan struct{}
	writeObserved  chan string
	writes         []string
	onWrite        func(*fakePort, []byte)
	activeReads    int
	maxActiveReads int
	readCalls      int
}

func newFakePort() *fakePort {
	return &fakePort{
		reads:         make(chan fakeRead, 4096),
		closed:        make(chan struct{}),
		readObserved:  make(chan struct{}, 1024),
		writeObserved: make(chan string, 1024),
	}
}

func (p *fakePort) setOnWrite(onWrite func(*fakePort, []byte)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onWrite = onWrite
}

func (p *fakePort) Read(data []byte) (int, error) {
	p.mu.Lock()
	p.activeReads++
	if p.activeReads > p.maxActiveReads {
		p.maxActiveReads = p.activeReads
	}
	p.readCalls++
	p.mu.Unlock()
	select {
	case p.readObserved <- struct{}{}:
	default:
	}
	defer func() {
		p.mu.Lock()
		p.activeReads--
		p.mu.Unlock()
	}()

	item, ok, err := p.nextRead()
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, io.EOF
	}
	if item.err != nil {
		return 0, item.err
	}
	data[0] = item.b
	n := 1
	for n < len(data) {
		select {
		case item := <-p.reads:
			if item.err != nil {
				return n, nil
			}
			data[n] = item.b
			n++
		case <-p.closed:
			return n, nil
		default:
			return n, nil
		}
	}
	return n, nil
}

func (p *fakePort) nextRead() (fakeRead, bool, error) {
	select {
	case item := <-p.reads:
		return item, true, nil
	case <-p.closed:
		return fakeRead{}, false, nil
	}
}

func (p *fakePort) Write(data []byte) (int, error) {
	copied := append([]byte(nil), data...)
	value := string(copied)

	p.mu.Lock()
	p.writes = append(p.writes, value)
	onWrite := p.onWrite
	p.mu.Unlock()

	p.writeObserved <- value
	if onWrite != nil {
		onWrite(p, copied)
	}
	return len(data), nil
}

func (p *fakePort) Close() error {
	p.closeOnce.Do(func() {
		close(p.closed)
	})
	return nil
}

func (p *fakePort) enqueueLine(line string) {
	for _, b := range []byte(line + "\n") {
		p.reads <- fakeRead{b: b}
	}
}

func (p *fakePort) injectError(err error) {
	p.reads <- fakeRead{err: err}
}

func (p *fakePort) waitWrite(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case got := <-p.writeObserved:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for write %q; writes=%v", want, p.writesSnapshot())
		}
	}
}

func (p *fakePort) waitRead(t *testing.T) {
	t.Helper()
	select {
	case <-p.readObserved:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for read")
	}
}

func (p *fakePort) countWrite(want string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, got := range p.writes {
		if got == want {
			count++
		}
	}
	return count
}

func (p *fakePort) writesSnapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.writes...)
}

func (p *fakePort) readCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.readCalls
}

func (p *fakePort) maxActiveReadCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxActiveReads
}
