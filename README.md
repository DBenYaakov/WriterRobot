# WriterRobot

Open-source tools for controlling the Writing Robot T-A4 from macOS.

`ta4-send` streams G-code to the robot's GRBL controller one command at a time and waits for each `ok` response. Every session begins by homing the machine and dwelling briefly.

## Machine profile

- Controller: GRBL
- Serial speed: 115200 baud
- Machine home: upper-left
- Paper origin: calibrated per sheet
- X+: right
- Y-: down the paper
- Default pen up: `Z0.50`
- Default pen down: `Z1.70`
- Drawing feed: `F600`

## Build

```bash
go mod tidy
go test ./...
go build -o ta4-send ./cmd/ta4-send
```

## Calibrate the pen and starting position

Run:

```bash
./.build/ta4-send \
  --port /dev/cu.usbmodem201912341 \
  --calibrate
```

Calibration now has two stages after the machine homes:

1. **Pen-down height**
   - Up arrow raises the pen by `--calibration-step`.
   - Down arrow lowers the pen by `--calibration-step`.
   - Enter accepts the pen-down value and raises the pen.
2. **Starting position**
   - Arrow keys move X/Y by `--position-step` (default `1.0` mm).
   - `U` raises the pen.
   - `D` lowers the pen, which is useful for checking the exact mark location.
   - Enter saves X/Y and raises the pen.

Escape or Ctrl-C cancels without saving. Example with finer increments:

```bash
./.build/ta4-send --port /dev/cu.usbmodem201912341 --calibrate \
  --calibration-step 0.01 --position-step 0.25
```

The saved JSON configuration contains `pen_up`, `pen_down`, `start_x`, `start_y`, `machine_home_x`, `machine_home_y`, and `return_home_on_completion`. G-code files may use `{{PEN_UP}}`, `{{PEN_DOWN}}`, `{{START_X}}`, and `{{START_Y}}` placeholders.

## Configured pen positions in G-code

WriterRobot G-code can use these placeholders:

```gcode
G1 Z{{PEN_UP}} F300
G1 Z{{PEN_DOWN}} F200
```

`ta4-send` replaces them with the saved values before sending commands to GRBL. `testdata/square.gcode` uses these placeholders, so a calibration immediately affects the next square test.

## Run the square test

```bash
./ta4-send \
  --port /dev/cu.usbmodem201912341 \
  testdata/square.gcode
```

Every session automatically sends:

```gcode
$H
G4 P0.300
```

The sender stops on `error:` or `ALARM:` and polls until the machine reports `Idle` after the last drawing command. On normal completion, the session clears the temporary paper-origin offset, raises the pen, and returns to the configured machine home with a `G53` rapid move. The default configuration enables this return home behavior with machine home at `X0 Y0`; setting `return_home_on_completion` to `false` in the config keeps the machine at the drawing completion position.

## Plot an SVG

`ta4-send` can import a local SVG directly:

```bash
./.build/ta4-send \
  --port /dev/cu.usbmodem201912341 \
  --svg drawing.svg
```

Supported SVG geometry is `path`, `line`, `polyline`, `polygon`, `rect`, `circle`, and `ellipse`. Path import supports `M`, `L`, `H`, `V`, `C`, `S`, `Q`, `T`, and `Z`, including relative lowercase forms. Curves are flattened into line segments before streaming to GRBL.

Useful sizing options:

```bash
./.build/ta4-send \
  --port /dev/cu.usbmodem201912341 \
  --svg drawing.svg \
  --svg-fit-width 80 \
  --work-width 100 \
  --work-height 100
```

SVG coordinates are transformed into WriterRobot program coordinates with X increasing right and Y increasing downward on paper as negative program Y. By default, imported SVG geometry is uniformly scaled to fit within the configured `--work-width` and `--work-height`, preserving aspect ratio, then left/top aligned at the calibrated paper origin. The sender preflights the final fitted bounds before homing or moving the machine and rejects unsupported SVG features, malformed coordinates, empty geometry, and drawings outside the configured work area.

## SVG fixture suite

Checked-in SVG fixtures live in `testdata/svg/`. The numbered files each isolate one SVG import or geometry-processing behavior: lines, rectangles, circles, ellipses, polylines, polygons, closed paths, relative paths, cubic and quadratic Bezier flattening, multiple disconnected strokes, transforms, nested transforms, viewBox scaling, and a small signature-like drawing. Invalid and unsupported fixtures in the same directory exercise malformed XML, malformed path data, non-finite coordinates, empty SVGs, text, images, and clip paths.

The standard manual SVG hardware check is:

```bash
./.build/ta4-send \
  --port /dev/cu.usbmodem201912341 \
  --svg testdata/svg/hardware-check.svg \
  --work-width 100 \
  --work-height 100
```

`hardware-check.svg` contains one rectangle, one circle, one triangle, one cubic Bezier curve, and disconnected paths. Its default plotted geometry bounds are 70 mm wide by 50 mm tall.

For incremental hardware validation, plot these fixtures in order: `02-rectangle.svg`, `03-circle.svg`, `07-triangle.svg`, `08-cubic-bezier.svg`, `12-multiple-strokes.svg`, `16-simple-signature.svg`, then `hardware-check.svg`.

## Internal plotting architecture

The SVG plotting path is deliberately layered:

- `internal/svg` reads XML, parses supported SVG elements and path commands, and returns neutral source-coordinate vector geometry plus document metadata such as `viewBox`, width, and height.
- `internal/drawing` defines neutral geometry types only. It does not know about SVG, G-code, GRBL, machine control, sessions, or CLI flags.
- `internal/geometry` applies transforms, flattens curves, handles `viewBox` and SVG sizing, scales or fits geometry, inverts Y into WriterRobot program coordinates, computes bounds, and performs work-area preflight.
- `internal/plot` turns processed drawings into ordered pen-up, rapid-move, pen-down, and drawing-move operations. It first merges already-contiguous open strokes, then uses constrained nearest-neighbor planning: only the first three remaining source-order strokes are considered, and any stroke bypassed twice becomes mandatory. Open strokes may be drawn in reverse when that endpoint is closer to the current pen position. Closed strokes are never reversed, but they may rotate to enter at the nearest vertex while preserving the original orientation. Equal-distance ties keep the original document order, original direction, and earliest closed-path vertex.
- `internal/machine`, `internal/session`, and `internal/grbl` own machine command formatting, session lifecycle, and controller transport.

## Safety

The robot moves to its upper-left machine home position immediately after initialization and, by default, returns there after a successful plotting session. Keep the travel path clear and keep a hand near the power switch during calibration and early tests. Lower the pen in small increments to avoid forcing the mechanism into the paper.

## Generate the Bezier calibration pattern

Build both commands:

```bash
go build -o ta4-send ./cmd/ta4-send
go build -o ta4-bezier-test ./cmd/ta4-bezier-test
```

Generate the default four-row adaptive-flattening test:

```bash
./ta4-bezier-test
```

This writes `testdata/bezier-calibration.gcode`. From top to bottom, the rows use tolerances of `0.50`, `0.25`, `0.10`, and `0.05` mm. Each row contains a shallow curve, a tight curve, a continuous S-curve, and a closed loop. The generator prints the number of line segments used for each row.

Send the generated test using the saved pen calibration:

```bash
./ta4-send \
  --port /dev/cu.usbmodem201912341 \
  testdata/bezier-calibration.gcode
```

To test different tolerances or placement:

```bash
./ta4-bezier-test \
  --tolerances 0.40,0.20,0.10,0.05 \
  --x 25 \
  --y -20 \
  --row-spacing 35
```

The `--y` value identifies the top of the first row. Because the machine homes at the upper-left and moves down the paper with negative Y coordinates, subsequent rows are placed at increasingly negative Y values.

## Generate the motion and feed-rate calibration pattern

The generalized `build.sh` discovers and builds every command under `cmd/`:

```bash
./build.sh
```

Generate the default six-row motion test:

```bash
./.build/ta4-motion-test
```

This writes `testdata/motion-calibration.gcode`. From top to bottom, the rows use drawing feed rates of `200`, `400`, `600`, `800`, `1000`, and `1200` mm/min. Each row draws:

- a horizontal line
- a vertical line
- a 45-degree diagonal
- a sharp-corner box
- a 4 mm circle
- an 18 mm circle
- a tightening spiral
- a continuous sweeping S-curve

Send the pattern using the saved pen calibration:

```bash
./.build/ta4-send \
  --port /dev/cu.usbmodem201912341 \
  testdata/motion-calibration.gcode
```

The rows are intentionally identical except for feed rate. Compare line straightness, corner overshoot, visible vibration, circle roundness, spiral consistency, and hesitation along the S-curve. Stop the machine if a higher-speed row begins to chatter or flex excessively.

Customize placement or feed rates as needed:

```bash
./.build/ta4-motion-test \
  --feeds 300,450,600,750,900 \
  --x 20 \
  --y -20 \
  --row-spacing 40
```

## Planned handwriting stroke profile

The SVG-to-G-code stage should support a `handwriting` stroke profile that varies feed rate with local curvature: faster on long straight sweeps and slower through tight curves and direction changes. With a gel pen, this also varies ink prominence in a way that more closely resembles a human signature.
