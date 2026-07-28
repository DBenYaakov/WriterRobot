package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DBenYaakov/WriterRobot/internal/config"
	"github.com/DBenYaakov/WriterRobot/internal/grbl"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
)

func TestReadKey(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  key
	}{
		{"enter", "\r", keyEnter},
		{"ctrl-c", string([]byte{3}), keyCancel},
		{"up", "\x1b[A", keyUp},
		{"down", "\x1b[B", keyDown},
		{"right", "\x1b[C", keyRight},
		{"left", "\x1b[D", keyLeft},
		{"pen up lower", "u", keyPenUp},
		{"pen up upper", "U", keyPenUp},
		{"pen down lower", "d", keyPenDown},
		{"pen down upper", "D", keyPenDown},
		{"circles lower", "c", keyPatternCircles},
		{"circles upper", "C", keyPatternCircles},
		{"squares lower", "s", keyPatternSquares},
		{"triangles lower", "t", keyPatternTriangles},
		{"sine lower", "v", keyPatternSine},
		{"grid lower", "g", keyPatternGrid},
		{"crosshair lower", "x", keyPatternCrosshair},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readKey(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("readKey: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInterruptDuringStreamingRunsRecovery(t *testing.T) {
	port := newScriptedPort(func(p *scriptedPort, data []byte) {
		switch string(data) {
		case "?":
			if p.statusCount() == 1 {
				p.enqueue("<Hold:0|MPos:1.000,2.000,0.000>\n")
			} else {
				p.enqueue("<Idle|MPos:1.000,2.000,0.500>\n")
			}
		case string([]byte{0x18}):
			p.enqueue("Grbl 1.1h ['$' for help]\n")
		default:
			if strings.HasSuffix(string(data), "\n") {
				p.enqueue("ok\n")
			}
		}
	})
	sender := testSender(port)
	_, _, recovery := testMachine(sender)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recoverAfterInterrupt(ctx, recovery, context.Canceled)

	report := recovery.report
	if !report.feedHoldSent || report.feedHoldErr != nil {
		t.Fatalf("feed hold report = sent %v err %v", report.feedHoldSent, report.feedHoldErr)
	}
	if !report.resetSent || report.resetErr != nil {
		t.Fatalf("reset report = sent %v err %v", report.resetSent, report.resetErr)
	}
	if !report.stopStateConfirmed || stateBase(report.stopState) != "Hold" {
		t.Fatalf("stop state = confirmed %v state %q", report.stopStateConfirmed, report.stopState)
	}
	if !report.penUpAttempted || report.penUpErr != nil {
		t.Fatalf("pen-up report = attempted %v err %v", report.penUpAttempted, report.penUpErr)
	}
	if !report.finalConfirmed || report.finalState != "Idle" {
		t.Fatalf("final state = confirmed %v state %q", report.finalConfirmed, report.finalState)
	}
	if !port.sawWrite("!") {
		t.Fatal("feed hold byte was not written")
	}
	if !port.sawWrite(string([]byte{0x18})) {
		t.Fatal("soft reset byte was not written")
	}
	if !port.sawCommand("G1 Z0.500 F300") {
		t.Fatal("pen-up command was not written")
	}
}

func TestCalibrationCancellationWhilePenDownRaisesPen(t *testing.T) {
	port := newScriptedPort(func(p *scriptedPort, data []byte) {
		switch string(data) {
		case "?":
			p.enqueue("<Idle|MPos:0.000,0.000,0.500>\n")
		default:
			if strings.HasSuffix(string(data), "\n") {
				p.enqueue("ok\n")
			}
		}
	})
	sender := testSender(port)
	robot, pen, recovery := testMachine(sender)
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7, StartX: 10, StartY: -20}

	err := calibrateStartPositionFrom(context.Background(), sender, robot, pen, &cfg, 1, recovery, strings.NewReader("d\x03"))
	if err != context.Canceled {
		t.Fatalf("calibrateStartPositionFrom error = %v, want context.Canceled", err)
	}
	if !port.sawCommand("G1 Z1.700 F200") {
		t.Fatal("pen-down command was not written")
	}
	if !port.sawCommand("G1 Z0.500 F300") {
		t.Fatal("pen-up cleanup command was not written")
	}
}

func TestCalibrationStartPositionCanFinishWithoutDiagnostics(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	robot, pen, recovery := testMachine(sender)
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7, StartX: 10, StartY: -20}

	err := calibrateStartPositionFrom(context.Background(), sender, robot, pen, &cfg, 1, recovery, strings.NewReader("\r\r"))
	if err != nil {
		t.Fatalf("calibrateStartPositionFrom: %v", err)
	}
	if cfg.StartX != 10 || cfg.StartY != -20 {
		t.Fatalf("start = X%.3f Y%.3f, want X10.000 Y-20.000", cfg.StartX, cfg.StartY)
	}
	if !port.sawCommand("G0 X10.000 Y-20.000") {
		t.Fatal("initial start-position move was not written")
	}
	if !port.sawCommand("G92 X0 Y0") {
		t.Fatal("program origin command was not written")
	}
	if port.sawCommand("G1 X60.000 Y0.000 F600") {
		t.Fatal("diagnostic pattern was drawn without selecting one")
	}
}

func TestCalibrationDiagnosticsDrawSelectedPattern(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	robot, pen, recovery := testMachine(sender)
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7, StartX: 10, StartY: -20}

	err := calibrateStartPositionFrom(context.Background(), sender, robot, pen, &cfg, 1, recovery, strings.NewReader("\rx\r"))
	if err != nil {
		t.Fatalf("calibrateStartPositionFrom: %v", err)
	}
	if !port.sawCommand("G92 X0 Y0") {
		t.Fatal("program origin command was not written before diagnostics")
	}
	if !port.sawCommand("G1 Z0.500 F300") {
		t.Fatal("diagnostic pattern did not begin with pen raised")
	}
	if !port.sawCommand("G1 X60.000 Y0.000 F600") {
		t.Fatal("crosshair horizontal axis was not drawn")
	}
	if !port.sawCommand("G1 X0.000 Y-60.000 F600") {
		t.Fatal("crosshair vertical axis was not drawn")
	}
	if !port.sawCommand("G0 X0.000 Y0.000") {
		t.Fatal("diagnostic pattern did not return to program origin")
	}
}

func TestPaperOriginCommandsRemainMachineMoveThenG92(t *testing.T) {
	rec := &commandRecorder{}
	robot := machine.New(rec)
	cfg := config.Config{StartX: 10.125, StartY: -20.5}

	if err := robot.MoveMachineXYTo(context.Background(), cfg.StartX, cfg.StartY); err != nil {
		t.Fatalf("MoveMachineXYTo: %v", err)
	}
	if err := robot.SetProgramXYOrigin(context.Background()); err != nil {
		t.Fatalf("SetProgramXYOrigin: %v", err)
	}
	want := []string{"G53 G0 X10.125 Y-20.500", "G92 X0 Y0"}
	if strings.Join(rec.commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands = %v, want %v", rec.commands, want)
	}
}

func TestPenUpCleanupSuccess(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	_, _, recovery := testMachine(sender)

	report := recovery.penUpOnly()
	if !report.penUpAttempted {
		t.Fatal("pen-up was not attempted")
	}
	if report.penUpErr != nil {
		t.Fatalf("pen-up error = %v", report.penUpErr)
	}
	if !report.finalConfirmed || report.finalState != "Idle" {
		t.Fatalf("final state = confirmed %v state %q", report.finalConfirmed, report.finalState)
	}
	if !port.sawCommand("G21") {
		t.Fatal("millimeter units command was not written")
	}
	if !port.sawCommand("G1 Z0.500 F300") {
		t.Fatal("pen-up command was not written")
	}
}

func TestPenUpCleanupFailure(t *testing.T) {
	port := newScriptedPort(func(p *scriptedPort, data []byte) {
		command := strings.TrimSpace(string(data))
		switch {
		case string(data) == "?":
			p.enqueue("<Idle|MPos:0.000,0.000,0.000>\n")
		case command == "G1 Z0.500 F300":
			p.enqueue("error:1\n")
		case strings.HasSuffix(string(data), "\n"):
			p.enqueue("ok\n")
		}
	})
	sender := testSender(port)
	_, _, recovery := testMachine(sender)

	report := recovery.penUpOnly()
	if !report.penUpAttempted {
		t.Fatal("pen-up was not attempted")
	}
	if report.penUpErr == nil {
		t.Fatal("pen-up failure was not reported")
	}
}

func TestFeedHoldTransmission(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	_, _, recovery := testMachine(sender)

	report := recovery.interrupt()
	if !report.feedHoldSent || report.feedHoldErr != nil {
		t.Fatalf("feed hold report = sent %v err %v", report.feedHoldSent, report.feedHoldErr)
	}
	if !port.sawWrite("!") {
		t.Fatal("feed hold byte was not written")
	}
}

func TestCleanupOnlyOnce(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	_, _, recovery := testMachine(sender)

	recovery.penUpOnly()
	recovery.penUpOnly()

	if got := port.countCommand("G1 Z0.500 F300"); got != 1 {
		t.Fatalf("pen-up command count = %d, want 1", got)
	}
}

func TestConcurrentMachineRecoveryCallersReceiveCompletedReport(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	_, _, recovery := testMachine(sender)

	const callers = 8
	var wg sync.WaitGroup
	reports := make(chan recoveryReport, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports <- recovery.penUpOnly()
		}()
	}
	wg.Wait()
	close(reports)

	for report := range reports {
		if !report.penUpAttempted || report.penUpErr != nil || !report.finalConfirmed {
			t.Fatalf("incomplete report: %+v", report)
		}
	}
	if got := port.countCommand("G1 Z0.500 F300"); got != 1 {
		t.Fatalf("pen-up command count = %d, want 1", got)
	}
}

func testSender(port *scriptedPort) *grbl.Sender {
	return grbl.New(port, grbl.Options{
		CommandTimeout: 100 * time.Millisecond,
		IdleTimeout:    100 * time.Millisecond,
		PollInterval:   time.Millisecond,
		Log:            io.Discard,
	})
}

func testMachine(sender *grbl.Sender) (*machine.Machine, *machine.Pen, *machineRecovery) {
	robot := machine.New(sender)
	pen := machine.NewPen(robot, 0.5, 1.7)
	recovery := newMachineRecovery(sender, robot, pen, io.Discard, recoveryTimings{
		holdWait:       100 * time.Millisecond,
		resetWait:      100 * time.Millisecond,
		commandWait:    100 * time.Millisecond,
		finalStateWait: 100 * time.Millisecond,
	})
	return robot, pen, recovery
}

type commandRecorder struct {
	commands []string
}

func (r *commandRecorder) Command(_ context.Context, command string) error {
	r.commands = append(r.commands, command)
	return nil
}

func newOKScriptedPort() *scriptedPort {
	return newScriptedPort(func(p *scriptedPort, data []byte) {
		switch string(data) {
		case "?":
			p.enqueue("<Idle|MPos:0.000,0.000,0.500>\n")
		case string([]byte{0x18}):
			p.enqueue("Grbl 1.1h ['$' for help]\n")
		default:
			if strings.HasSuffix(string(data), "\n") {
				p.enqueue("ok\n")
			}
		}
	})
}

type scriptedPort struct {
	mu        sync.Mutex
	closeOnce sync.Once
	reads     chan byte
	writes    []string
	statuses  int
	onWrite   func(*scriptedPort, []byte)
}

func newScriptedPort(onWrite func(*scriptedPort, []byte)) *scriptedPort {
	return &scriptedPort{
		reads:   make(chan byte, 4096),
		onWrite: onWrite,
	}
}

func (p *scriptedPort) Read(data []byte) (int, error) {
	b, ok := <-p.reads
	if !ok {
		return 0, io.EOF
	}
	data[0] = b
	n := 1
	for n < len(data) {
		select {
		case b, ok := <-p.reads:
			if !ok {
				return n, nil
			}
			data[n] = b
			n++
		default:
			return n, nil
		}
	}
	return n, nil
}

func (p *scriptedPort) Write(data []byte) (int, error) {
	copied := append([]byte(nil), data...)
	p.mu.Lock()
	p.writes = append(p.writes, string(copied))
	if string(copied) == "?" {
		p.statuses++
	}
	p.mu.Unlock()
	if p.onWrite != nil {
		p.onWrite(p, copied)
	}
	return len(data), nil
}

func (p *scriptedPort) Close() error {
	p.closeOnce.Do(func() {
		close(p.reads)
	})
	return nil
}

func (p *scriptedPort) enqueue(line string) {
	for _, b := range []byte(line) {
		p.reads <- b
	}
}

func (p *scriptedPort) statusCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statuses
}

func (p *scriptedPort) sawWrite(want string) bool {
	return p.countWrite(want) > 0
}

func (p *scriptedPort) countWrite(want string) int {
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

func (p *scriptedPort) sawCommand(want string) bool {
	return p.countCommand(want) > 0
}

func (p *scriptedPort) countCommand(want string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, got := range p.writes {
		if strings.TrimSpace(got) == want {
			count++
		}
	}
	return count
}
