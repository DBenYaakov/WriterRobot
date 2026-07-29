package plot

import (
	"fmt"
	"math"
	"sort"

	"github.com/DBenYaakov/WriterRobot/internal/drawing"
)

const (
	fastDrawingFeedScale = 1.35
	slowDrawingFeedScale = 0.40
)

type drawingFeedLevel int

const (
	feedSlow drawingFeedLevel = iota
	feedNormal
	feedFast
)

// DrawFeedTime stores estimated pen-down drawing time for one feed mode.
type DrawFeedTime struct {
	Feed     float64
	Distance float64
	Seconds  float64
	Moves    int
}

// DrawFeedTimeSummary stores estimated pen-down drawing time by feed mode.
type DrawFeedTimeSummary struct {
	Slow   DrawFeedTime
	Normal DrawFeedTime
	Fast   DrawFeedTime
	Other  DrawFeedTime
}

// TotalSeconds returns the total estimated pen-down drawing time.
func (s DrawFeedTimeSummary) TotalSeconds() float64 {
	return s.Slow.Seconds + s.Normal.Seconds + s.Fast.Seconds + s.Other.Seconds
}

// CurvatureBand stores distance-weighted curvature information for one feed band.
type CurvatureBand struct {
	Feed       float64
	MinDegrees float64
	MaxDegrees float64
	Distance   float64
	Moves      int
}

// CurvatureHistogram stores the curvature distribution used for feed selection.
type CurvatureHistogram struct {
	Slow   CurvatureBand
	Normal CurvatureBand
	Fast   CurvatureBand
	Other  CurvatureBand
}

// TotalDistance returns the total pen-down distance represented by the histogram.
func (h CurvatureHistogram) TotalDistance() float64 {
	return h.Slow.Distance + h.Normal.Distance + h.Fast.Distance + h.Other.Distance
}

// AnalyzeDrawFeeds estimates pen-down drawing time by effective feed mode.
func AnalyzeDrawFeeds(ops []Operation, normalFeed float64) (DrawFeedTimeSummary, error) {
	if normalFeed <= 0 || !isFinite(normalFeed) {
		return DrawFeedTimeSummary{}, fmt.Errorf("normal draw feed must be finite and greater than zero")
	}
	stats := DrawFeedTimeSummary{
		Slow:   DrawFeedTime{Feed: slowDrawingFeed(normalFeed)},
		Normal: DrawFeedTime{Feed: normalDrawingFeed(normalFeed)},
		Fast:   DrawFeedTime{Feed: fastDrawingFeed(normalFeed)},
	}
	currentPoint := drawing.Point{}
	currentFeed := 0.0
	for i, op := range ops {
		switch op.Kind {
		case OperationPenUp, OperationPenDown:
			currentFeed = 0
		case OperationRapidMove:
			currentPoint = op.Point
			currentFeed = 0
		case OperationDrawMove:
			if op.Feed > 0 {
				if !isFinite(op.Feed) {
					return DrawFeedTimeSummary{}, fmt.Errorf("operation %d draw feed must be finite", i+1)
				}
				currentFeed = op.Feed
			}
			if currentFeed <= 0 {
				return DrawFeedTimeSummary{}, fmt.Errorf("operation %d draw move has no effective feed", i+1)
			}
			length := distance(currentPoint, op.Point)
			stats.addDrawMove(currentFeed, length)
			currentPoint = op.Point
		}
	}
	return stats, nil
}

// AnalyzeCurvature returns the distance-weighted curvature histogram for a plan.
func AnalyzeCurvature(ops []Operation, normalFeed, tolerance float64) (CurvatureHistogram, error) {
	if normalFeed <= 0 || !isFinite(normalFeed) {
		return CurvatureHistogram{}, fmt.Errorf("normal draw feed must be finite and greater than zero")
	}
	if tolerance < 0 || !isFinite(tolerance) {
		return CurvatureHistogram{}, fmt.Errorf("curvature tolerance must be finite and non-negative")
	}
	segments := drawSegmentsWithCurvature(ops, tolerance)
	histogram := CurvatureHistogram{
		Slow:   CurvatureBand{Feed: slowDrawingFeed(normalFeed)},
		Normal: CurvatureBand{Feed: normalDrawingFeed(normalFeed)},
		Fast:   CurvatureBand{Feed: fastDrawingFeed(normalFeed)},
	}
	levels := curvatureTercileLevels(segments)
	for i, segment := range segments {
		histogram.addSegment(levels[i], segment)
	}
	return histogram, nil
}

func (h *CurvatureHistogram) addSegment(level drawingFeedLevel, segment drawSegment) {
	bucket := h.levelBucket(level)
	bucket.addSegment(segment)
}

func (h *CurvatureHistogram) levelBucket(level drawingFeedLevel) *CurvatureBand {
	switch level {
	case feedSlow:
		return &h.Slow
	case feedFast:
		return &h.Fast
	default:
		return &h.Normal
	}
}

func (b *CurvatureBand) addSegment(segment drawSegment) {
	degrees := radiansToDegrees(segment.curvature)
	if b.Moves == 0 {
		b.MinDegrees = degrees
		b.MaxDegrees = degrees
	} else {
		if degrees < b.MinDegrees {
			b.MinDegrees = degrees
		}
		if degrees > b.MaxDegrees {
			b.MaxDegrees = degrees
		}
	}
	b.Moves++
	b.Distance += segment.length
}

func (s *DrawFeedTimeSummary) addDrawMove(feed, length float64) {
	bucket := s.feedBucket(feed)
	bucket.Moves++
	bucket.Distance += length
	bucket.Seconds += length / feed * 60
}

func (s *DrawFeedTimeSummary) feedBucket(feed float64) *DrawFeedTime {
	switch {
	case sameFeed(feed, s.Slow.Feed):
		return &s.Slow
	case sameFeed(feed, s.Normal.Feed):
		return &s.Normal
	case sameFeed(feed, s.Fast.Feed):
		return &s.Fast
	default:
		return &s.Other
	}
}

func annotateFixedDrawFeeds(ops []Operation, normalFeed float64) {
	currentFeed := 0.0
	for i := range ops {
		if ops[i].Kind != OperationDrawMove {
			currentFeed = 0
			continue
		}
		if currentFeed <= 0 || !sameFeed(normalFeed, currentFeed) {
			ops[i].Feed = normalFeed
			currentFeed = normalFeed
		} else {
			ops[i].Feed = 0
		}
	}
}

func annotateCurvatureFeeds(ops []Operation, normalFeed, tolerance float64) {
	segments := drawSegmentsWithCurvature(ops, tolerance)
	if len(segments) == 0 {
		return
	}
	levels := smoothCurvatureLevels(curvatureTercileLevels(segments))
	feedsByOperation := map[int]float64{}
	for i, segment := range segments {
		feedsByOperation[segment.opIndex] = feedForLevel(levels[i], normalFeed)
	}
	currentFeed := 0.0
	for i := range ops {
		if ops[i].Kind != OperationDrawMove {
			currentFeed = 0
			continue
		}
		feed := feedsByOperation[i]
		if currentFeed <= 0 || !sameFeed(feed, currentFeed) {
			ops[i].Feed = feed
			currentFeed = feed
		} else {
			ops[i].Feed = 0
		}
	}
}

type drawSegment struct {
	opIndex   int
	start     drawing.Point
	end       drawing.Point
	length    float64
	curvature float64
	feed      float64
}

func drawSegmentsWithCurvature(ops []Operation, tolerance float64) []drawSegment {
	var segments []drawSegment
	var stroke []drawSegment
	currentPoint := drawing.Point{}
	currentFeed := 0.0
	flushStroke := func() {
		if len(stroke) == 0 {
			return
		}
		segments = append(segments, strokeSegmentsWithCurvature(stroke, tolerance)...)
		stroke = nil
	}
	for i, op := range ops {
		switch op.Kind {
		case OperationPenUp, OperationPenDown:
			flushStroke()
			currentFeed = 0
		case OperationRapidMove:
			flushStroke()
			currentPoint = op.Point
			currentFeed = 0
		case OperationDrawMove:
			if op.Feed > 0 {
				currentFeed = op.Feed
			}
			stroke = append(stroke, drawSegment{
				opIndex: i,
				start:   currentPoint,
				end:     op.Point,
				length:  distance(currentPoint, op.Point),
				feed:    currentFeed,
			})
			currentPoint = op.Point
		}
	}
	flushStroke()
	return segments
}

func strokeSegmentsWithCurvature(segments []drawSegment, tolerance float64) []drawSegment {
	points := make([]drawing.Point, 0, len(segments)+1)
	points = append(points, segments[0].start)
	for _, segment := range segments {
		points = append(points, segment.end)
	}
	vertexCurvatures := make([]float64, len(points))
	closed := samePointWithin(points[0], points[len(points)-1], tolerance)
	if closed {
		vertexCount := len(points) - 1
		if vertexCount >= 3 {
			for i := 0; i < vertexCount; i++ {
				vertexCurvatures[i] = curvatureAngle(
					points[(i-1+vertexCount)%vertexCount],
					points[i],
					points[(i+1)%vertexCount],
					tolerance,
				)
			}
			vertexCurvatures[len(vertexCurvatures)-1] = vertexCurvatures[0]
		}
	} else {
		for i := 1; i < len(points)-1; i++ {
			vertexCurvatures[i] = curvatureAngle(points[i-1], points[i], points[i+1], tolerance)
		}
	}
	result := make([]drawSegment, len(segments))
	copy(result, segments)
	for i := range result {
		result[i].curvature = math.Max(vertexCurvatures[i], vertexCurvatures[i+1])
	}
	return result
}

func curvatureTercileLevels(segments []drawSegment) []drawingFeedLevel {
	levels := make([]drawingFeedLevel, len(segments))
	totalDistance := 0.0
	for _, segment := range segments {
		totalDistance += segment.length
	}
	if totalDistance <= 0 {
		for i := range levels {
			levels[i] = feedNormal
		}
		return levels
	}
	ranked := make([]int, len(segments))
	for i := range ranked {
		ranked[i] = i
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a := segments[ranked[i]]
		b := segments[ranked[j]]
		if !sameCurvature(a.curvature, b.curvature) {
			return a.curvature < b.curvature
		}
		return ranked[i] < ranked[j]
	})
	cumulative := 0.0
	for _, index := range ranked {
		segment := segments[index]
		midpoint := cumulative + segment.length/2
		switch {
		case midpoint <= totalDistance/3:
			levels[index] = feedSlow
		case midpoint <= 2*totalDistance/3:
			levels[index] = feedNormal
		default:
			levels[index] = feedFast
		}
		cumulative += segment.length
	}
	return levels
}

func smoothCurvatureLevels(levels []drawingFeedLevel) []drawingFeedLevel {
	smoothed := append([]drawingFeedLevel(nil), levels...)
	for i := 1; i < len(smoothed)-1; i++ {
		if smoothed[i-1] == smoothed[i+1] && smoothed[i] != smoothed[i-1] {
			smoothed[i] = smoothed[i-1]
		}
	}
	for i := 1; i < len(smoothed); i++ {
		if directSlowFastTransition(smoothed[i-1], smoothed[i]) {
			smoothed[i] = feedNormal
		}
	}
	return smoothed
}

func directSlowFastTransition(a, b drawingFeedLevel) bool {
	return (a == feedSlow && b == feedFast) || (a == feedFast && b == feedSlow)
}

func curvatureAngle(prev, current, next drawing.Point, tolerance float64) float64 {
	inX := current.X - prev.X
	inY := current.Y - prev.Y
	outX := next.X - current.X
	outY := next.Y - current.Y
	inLength := math.Hypot(inX, inY)
	outLength := math.Hypot(outX, outY)
	if inLength <= tolerance || outLength <= tolerance {
		return 0
	}
	cosine := (inX*outX + inY*outY) / (inLength * outLength)
	if cosine > 1 {
		cosine = 1
	}
	if cosine < -1 {
		cosine = -1
	}
	return math.Acos(cosine)
}

func sameCurvature(a, b float64) bool {
	return math.Abs(a-b) <= 1e-9
}

func radiansToDegrees(radians float64) float64 {
	return radians * 180 / math.Pi
}

func feedForLevel(level drawingFeedLevel, normalFeed float64) float64 {
	switch level {
	case feedSlow:
		return slowDrawingFeed(normalFeed)
	case feedFast:
		return fastDrawingFeed(normalFeed)
	default:
		return normalDrawingFeed(normalFeed)
	}
}

func normalDrawingFeed(normalFeed float64) float64 {
	return normalFeed
}

func fastDrawingFeed(normalFeed float64) float64 {
	return normalFeed * fastDrawingFeedScale
}

func slowDrawingFeed(normalFeed float64) float64 {
	return normalFeed * slowDrawingFeedScale
}

func sameFeed(a, b float64) bool {
	return math.Abs(a-b) <= DefaultContiguousTolerance
}
