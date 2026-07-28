# WriterRobot

Open-source tools for controlling the Writing Robot T-A4 from macOS.

`ta4-send` streams G-code to the robot's GRBL controller one command at a time and waits for each `ok` response. Every session begins by homing the machine and dwelling briefly.

## Machine profile

- Controller: GRBL
- Serial speed: 115200 baud
- Home/origin: upper-left
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

## Calibrate the pen-down position

Disconnect any existing `screen` session, then run:

```bash
./ta4-send \
  --port /dev/cu.usbmodem201912341 \
  --calibrate
```

The calibration session automatically resets, homes, and dwells. It then moves to the currently configured pen-down position.

- **Down arrow:** lower the pen by 0.05 mm
- **Up arrow:** raise the pen by 0.05 mm
- **Enter:** save the selected position
- **Ctrl-C:** cancel without saving

Change the increment with `--calibration-step`, for example:

```bash
./ta4-send --port /dev/cu.usbmodem201912341 --calibrate --calibration-step 0.01
```

On macOS, the configuration is saved under the user's Application Support directory as `writerrobot/config.json`. The program prints the exact path after saving.

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

The sender stops on `error:` or `ALARM:` and polls until the machine reports `Idle` after the last command.

## Safety

The robot moves to its upper-left home position immediately after initialization. Keep the travel path clear and keep a hand near the power switch during calibration and early tests. Lower the pen in small increments to avoid forcing the mechanism into the paper.

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
