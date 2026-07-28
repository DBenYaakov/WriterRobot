; Writing Robot T-A4 square test
; Session homing and startup dwell are performed automatically by ta4-send.
; Pen positions come from the WriterRobot configuration file.
G21
G90
G1 Z{{PEN_UP}} F300
G0 X60 Y-20
G1 Z{{PEN_DOWN}} F200
G1 X90 Y-20 F600
G1 X90 Y-50
G1 X60 Y-50
G1 X60 Y-20
G1 Z{{PEN_UP}} F300
G4 P0.3
