package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DBenYaakov/WriterRobot/internal/config"
	"github.com/DBenYaakov/WriterRobot/internal/console"
	"github.com/DBenYaakov/WriterRobot/internal/gcode"
	"github.com/DBenYaakov/WriterRobot/internal/grbl"
	"github.com/DBenYaakov/WriterRobot/internal/machine"
	"github.com/DBenYaakov/WriterRobot/internal/plot"
	"github.com/DBenYaakov/WriterRobot/internal/session"
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
	_, _, _, recovery := testMachine(sender)

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
	robot, drawingSession, pen, recovery := testMachine(sender)
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7, StartX: 10, StartY: -20}

	err := calibrateStartPositionFrom(context.Background(), sender, drawingSession, robot, pen, &cfg, 1, recovery, strings.NewReader("d\x03"), testLogger())
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
	robot, drawingSession, pen, recovery := testMachine(sender)
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7, StartX: 10, StartY: -20}

	err := calibrateStartPositionFrom(context.Background(), sender, drawingSession, robot, pen, &cfg, 1, recovery, strings.NewReader("\r\r"), testLogger())
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
	if got := port.countCommand("G92.1"); got != 2 {
		t.Fatalf("program offset clear count = %d, want 2", got)
	}
	if port.sawCommand("G1 X60.000 Y0.000 F600") {
		t.Fatal("diagnostic pattern was drawn without selecting one")
	}
}

func TestCalibrationDiagnosticsCancellationClearsSession(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	robot, drawingSession, pen, recovery := testMachine(sender)
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7, StartX: 10, StartY: -20}

	err := calibrateStartPositionFrom(context.Background(), sender, drawingSession, robot, pen, &cfg, 1, recovery, strings.NewReader("\r\x03"), testLogger())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("calibrateStartPositionFrom error = %v, want context.Canceled", err)
	}
	if got := port.countCommand("G92.1"); got != 2 {
		t.Fatalf("program offset clear count = %d, want 2", got)
	}
}

func TestCalibrationDiagnosticsDrawSelectedPattern(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	robot, drawingSession, pen, recovery := testMachine(sender)
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7, StartX: 10, StartY: -20}

	err := calibrateStartPositionFrom(context.Background(), sender, drawingSession, robot, pen, &cfg, 1, recovery, strings.NewReader("\rx\r"), testLogger())
	if err != nil {
		t.Fatalf("calibrateStartPositionFrom: %v", err)
	}
	if !port.sawCommand("G92 X0 Y0") {
		t.Fatal("program origin command was not written before diagnostics")
	}
	if !port.sawCommand("G1 Z0.500 F300") {
		t.Fatal("diagnostic pattern did not begin with pen raised")
	}
	if !port.sawCommandPrefix("G1 X60.000 Y0.000") {
		t.Fatal("crosshair horizontal axis was not drawn")
	}
	if !port.sawCommandPrefix("G1 X0.000 Y-60.000") {
		t.Fatal("crosshair vertical axis was not drawn")
	}
	if !port.sawCommand("G0 X0.000 Y0.000") {
		t.Fatal("diagnostic pattern did not return to program origin")
	}
}

func TestPaperOriginSessionCommandsRemainOrdered(t *testing.T) {
	rec := &commandRecorder{}
	robot := machine.New(rec)
	drawingSession := session.New(robot)
	cfg := config.Config{StartX: 10.125, StartY: -20.5}

	if err := drawingSession.Begin(context.Background(), cfg.StartX, cfg.StartY); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	want := []string{"G21", "G90", "G17", "G94", "G54", "G92.1", "G53 G0 X10.125 Y-20.500", "G92 X0 Y0"}
	if strings.Join(rec.commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands = %v, want %v", rec.commands, want)
	}
}

func TestLoadSVGProgramGeneratesSafePlotterMotion(t *testing.T) {
	path := svgFixturePath("01-line.svg")
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7}

	lines, bounds, err := loadSVGProgram(path, cfg, svgProgramOptions{
		tolerance:  0.1,
		workWidth:  100,
		workHeight: 100,
		drawFeed:   600,
	})
	if err != nil {
		t.Fatalf("loadSVGProgram: %v", err)
	}
	if bounds.MinX != 0 || bounds.MaxX != 100 || bounds.MinY != -100 || bounds.MaxY != 0 {
		t.Fatalf("bounds = %+v, want X0..100 Y-100..0", bounds)
	}
	want := []string{
		"G1 Z0.500 F300",
		"G0 X0.000 Y0.000",
		"G1 Z1.700 F200",
		"G1 X100.000 Y-100.000 F600",
		"G1 Z0.500 F300",
		"G0 X0.000 Y0.000",
		"G1 Z0.500 F300",
	}
	got := make([]string, 0, len(lines))
	for _, line := range lines {
		got = append(got, line.Command)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands = %v, want %v", got, want)
	}
}

func TestLoadSVGProgramRejectsOutOfBoundsBeforeStreaming(t *testing.T) {
	path := svgFixturePath("12-multiple-strokes.svg")
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7}

	lines, _, err := loadSVGProgram(path, cfg, svgProgramOptions{
		tolerance:  0.1,
		fitWidth:   60,
		workWidth:  50,
		workHeight: 100,
		drawFeed:   600,
	})
	if err == nil {
		t.Fatal("loadSVGProgram succeeded, want bounds error")
	}
	if len(lines) != 0 {
		t.Fatalf("lines generated for rejected SVG: %v", lines)
	}
	if !strings.Contains(err.Error(), "exceed work bounds") {
		t.Fatalf("error = %v, want bounds context", err)
	}
}

func TestPrepareSVGProgramFitsBeforePreflight(t *testing.T) {
	path := svgFixturePath("17-inkscape-signature.svg")
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7}

	program, err := prepareSVGProgram(path, cfg, svgProgramOptions{
		tolerance:  0.1,
		workWidth:  100,
		workHeight: 100,
		drawFeed:   600,
	})
	if err != nil {
		t.Fatalf("prepareSVGProgram: %v", err)
	}
	if program.sourceBounds.Width() <= 100 {
		t.Fatalf("source width = %.3f, want larger than work width", program.sourceBounds.Width())
	}
	if program.bounds.MinX < -0.001 || program.bounds.MaxX > 100.001 || program.bounds.MinY < -100.001 || program.bounds.MaxY > 0.001 {
		t.Fatalf("final bounds = %+v, want inside 100x100 work area", program.bounds)
	}
	if program.bounds.Width() < 99 {
		t.Fatalf("final width = %.3f, want close to full 100 mm width", program.bounds.Width())
	}
	if program.bounds.Height() >= 100 {
		t.Fatalf("final height = %.3f, want less than full 100 mm height", program.bounds.Height())
	}
	if program.strokes != 29 {
		t.Fatalf("strokes = %d, want 29", program.strokes)
	}
	if len(program.lines) == 0 {
		t.Fatal("no G-code lines generated")
	}
	if program.signature {
		t.Fatal("signature mode enabled by default")
	}
	if program.feedStats.TotalSeconds() != 0 {
		t.Fatal("draw feed time statistics were computed without signature mode")
	}
	if program.curvature.TotalDistance() != 0 {
		t.Fatal("curvature histogram was computed without signature mode")
	}
}

func TestPrepareSVGProgramSignatureModeComputesFeedStats(t *testing.T) {
	path := svgFixturePath("17-inkscape-signature.svg")
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7}

	program, err := prepareSVGProgram(path, cfg, svgProgramOptions{
		tolerance:  0.1,
		workWidth:  100,
		workHeight: 100,
		drawFeed:   600,
		signature:  true,
	})
	if err != nil {
		t.Fatalf("prepareSVGProgram: %v", err)
	}
	if !program.signature {
		t.Fatal("signature mode was not recorded")
	}
	if program.feedStats.TotalSeconds() <= 0 {
		t.Fatal("draw feed time statistics were not computed")
	}
	if program.curvature.TotalDistance() <= 0 {
		t.Fatal("curvature histogram was not computed")
	}
	if !containsSVGCommandFeed(program.lines, "F240") && !containsSVGCommandFeed(program.lines, "F810") {
		t.Fatal("signature SVG did not include modulated draw feeds")
	}
}

func TestLogSVGPreparationReportsDrawFeedTimePercentages(t *testing.T) {
	var out bytes.Buffer
	log := console.New(&out)
	logSVGPreparation(log, svgProgram{
		signature: true,
		feedStats: plot.DrawFeedTimeSummary{
			Slow:   plot.DrawFeedTime{Feed: 240, Seconds: 2},
			Normal: plot.DrawFeedTime{Feed: 600, Seconds: 3},
			Fast:   plot.DrawFeedTime{Feed: 810, Seconds: 5},
		},
		curvature: plot.CurvatureHistogram{
			Slow:   plot.CurvatureBand{Feed: 240, Distance: 2, MinDegrees: 0, MaxDegrees: 5},
			Normal: plot.CurvatureBand{Feed: 600, Distance: 3, MinDegrees: 5, MaxDegrees: 15},
			Fast:   plot.CurvatureBand{Feed: 810, Distance: 5, MinDegrees: 15, MaxDegrees: 60},
		},
	})

	for _, want := range []string{
		"SVG curvature histogram by drawing distance: low/slow F240 20.0% (0.0..5.0 deg), middle/normal F600 30.0% (5.0..15.0 deg), high/fast F810 50.0% (15.0..60.0 deg)",
		"SVG estimated pen-down time by feed: slow F240 20.0%, normal F600 30.0%, fast F810 50.0% (total 10.0s)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("log output = %q, want %q", out.String(), want)
		}
	}
}

func TestLogSVGPreparationSuppressesSignatureStatsByDefault(t *testing.T) {
	var out bytes.Buffer
	log := console.New(&out)
	logSVGPreparation(log, svgProgram{
		feedStats: plot.DrawFeedTimeSummary{
			Slow: plot.DrawFeedTime{Feed: 240, Seconds: 2},
		},
		curvature: plot.CurvatureHistogram{
			Slow: plot.CurvatureBand{Feed: 240, Distance: 2},
		},
	})

	if strings.Contains(out.String(), "curvature histogram") {
		t.Fatalf("log output = %q, want no curvature histogram by default", out.String())
	}
	if strings.Contains(out.String(), "pen-down time") {
		t.Fatalf("log output = %q, want no feed timing by default", out.String())
	}
}

func TestLoadSVGProgramRejectsUnsupportedSVG(t *testing.T) {
	path := svgFixturePath("unsupported-text.svg")
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7}

	lines, _, err := loadSVGProgram(path, cfg, svgProgramOptions{
		tolerance:  0.1,
		workWidth:  100,
		workHeight: 100,
		drawFeed:   600,
	})
	if err == nil {
		t.Fatal("loadSVGProgram succeeded, want unsupported SVG error")
	}
	if len(lines) != 0 {
		t.Fatalf("lines generated for unsupported SVG: %v", lines)
	}
}

func TestLoadSVGProgramRejectsInvalidOptions(t *testing.T) {
	path := svgFixturePath("01-line.svg")
	cfg := config.Config{PenUp: 0.5, PenDown: 1.7}

	if _, _, err := loadSVGProgram(path, cfg, svgProgramOptions{tolerance: 0, workWidth: 100, workHeight: 100, drawFeed: 600}); err == nil {
		t.Fatal("loadSVGProgram succeeded with zero tolerance")
	}
}

func TestPenUpCleanupSuccess(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	_, _, _, recovery := testMachine(sender)

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
	if !port.sawCommand("G90") {
		t.Fatal("absolute positioning command was not written")
	}
	if !port.sawCommand("G92.1") {
		t.Fatal("program offset clear command was not written")
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
	_, _, _, recovery := testMachine(sender)

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
	_, _, _, recovery := testMachine(sender)

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
	_, _, _, recovery := testMachine(sender)

	recovery.penUpOnly()
	recovery.penUpOnly()

	if got := port.countCommand("G1 Z0.500 F300"); got != 1 {
		t.Fatalf("pen-up command count = %d, want 1", got)
	}
}

func TestConcurrentMachineRecoveryCallersReceiveCompletedReport(t *testing.T) {
	port := newOKScriptedPort()
	sender := testSender(port)
	_, _, _, recovery := testMachine(sender)

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

func testMachine(sender *grbl.Sender) (*machine.Machine, *session.Session, *machine.Pen, *machineRecovery) {
	robot := machine.New(sender)
	drawingSession := session.New(robot)
	pen := machine.NewPen(robot, 0.5, 1.7)
	recovery := newMachineRecovery(sender, drawingSession, pen, testLogger(), recoveryTimings{
		holdWait:       100 * time.Millisecond,
		resetWait:      100 * time.Millisecond,
		commandWait:    100 * time.Millisecond,
		finalStateWait: 100 * time.Millisecond,
	})
	return robot, drawingSession, pen, recovery
}

func testLogger() console.Logger {
	return console.New(io.Discard)
}

func svgFixturePath(file string) string {
	return filepath.Join("..", "..", "testdata", "svg", file)
}

func containsSVGCommandFeed(lines []gcode.Line, feed string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line.Command, "G1 X") && strings.Contains(line.Command, feed) {
			return true
		}
	}
	return false
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

func (p *scriptedPort) sawCommandPrefix(prefix string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, got := range p.writes {
		if strings.HasPrefix(strings.TrimSpace(got), prefix) {
			return true
		}
	}
	return false
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
