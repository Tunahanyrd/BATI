package analytics

import (
	"fmt"
	"math"
	"time"

	"bati/internal/db"
	"bati/internal/model"
)

// OvernightReport holds computed data about a sleep or idle de-charging session.
type OvernightReport struct {
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	StartPct   float64       `json:"start_pct"`
	EndPct     float64       `json:"end_pct"`
	Drain      float64       `json:"drain"` // e.g. -6.0
	Duration   time.Duration `json:"duration"`
	Type       string        `json:"type"`       // "Overnight Drain (Sleep Event Observed)" or "Estimated Overnight Drain (Screen-off Fallback)"
	Provenance string        `json:"provenance"` // "observed" or "estimated"
}

// CalculateOvernightDrain finds the latest overnight/sleep session and computes battery drop.
// Timestamps are handled in UTC throughout to prevent DST and ordering issues.
func CalculateOvernightDrain(database *db.DB) (*OvernightReport, error) {
	return CalculateOvernightDrainAt(database, time.Now())
}

// CalculateOvernightDrainAt computes overnight drain relative to a supplied
// instant. Supplying now keeps dashboard refreshes and tests internally
// consistent while CalculateOvernightDrain remains the CLI convenience entry.
func CalculateOvernightDrainAt(database *db.DB, now time.Time) (*OvernightReport, error) {
	now = now.UTC()
	yesterday := now.Add(-30 * time.Hour) // Lookback window for overnight session

	// 1. Fetch events in the window to find sleep/resume pairs
	events, err := database.GetEventsRange(yesterday, now)
	if err != nil {
		return nil, fmt.Errorf("fetch events: %w", err)
	}

	var sleepEvent, resumeEvent *model.Event

	// Find the latest resume event, and the closest preceding sleep event
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Type == "resume" {
			resumeEvent = &e
			// Search backwards for the sleep event preceding it
			for j := i - 1; j >= 0; j-- {
				prev := events[j]
				if prev.Type == "sleep" {
					sleepEvent = &prev
					break
				}
			}
			if sleepEvent != nil {
				break
			}
		}
	}

	if sleepEvent != nil && resumeEvent != nil {
		// Found explicit sleep/resume events. Get battery percentages around those times.
		startPct, err := getClosestCapacity(database, sleepEvent.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("get capacity for sleep: %w", err)
		}
		endPct, err := getClosestCapacity(database, resumeEvent.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("get capacity for resume: %w", err)
		}

		duration := resumeEvent.Timestamp.Sub(sleepEvent.Timestamp)
		if !isSufficientOvernightDuration(duration) {
			return nil, fmt.Errorf("observed sleep session is shorter than 4 hours")
		}
		return &OvernightReport{
			StartTime:  sleepEvent.Timestamp,
			EndTime:    resumeEvent.Timestamp,
			StartPct:   startPct,
			EndPct:     endPct,
			Drain:      endPct - startPct,
			Duration:   duration,
			Type:       "Overnight Drain (Sleep Event Observed)",
			Provenance: "observed",
		}, nil
	}

	// 2. Fallback: Find the longest contiguous period of screen_on = false in the last 24h
	telemetry, err := database.GetTelemetryRange(now.Add(-24*time.Hour), now)
	if err != nil {
		return nil, fmt.Errorf("fetch telemetry: %w", err)
	}

	if len(telemetry) < 2 {
		return nil, fmt.Errorf("insufficient data points to calculate overnight drain")
	}

	// Scan telemetry to find the longest stretch of screen_on = false
	var longestStart, longestEnd time.Time
	var longestStartPct, longestEndPct float64
	var maxDuration time.Duration

	var currentStart time.Time
	var currentStartPct float64
	inGap := false

	for _, t := range telemetry {
		if !t.ScreenOn {
			if !inGap {
				currentStart = t.Timestamp
				currentStartPct = t.Capacity
				inGap = true
			}
		} else {
			if inGap {
				duration := t.Timestamp.Sub(currentStart)
				if duration > maxDuration {
					maxDuration = duration
					longestStart = currentStart
					longestEnd = t.Timestamp
					longestStartPct = currentStartPct
					longestEndPct = t.Capacity
				}
				inGap = false
			}
		}
	}

	// Check final gap
	if inGap {
		lastPoint := telemetry[len(telemetry)-1]
		duration := lastPoint.Timestamp.Sub(currentStart)
		if duration > maxDuration {
			maxDuration = duration
			longestStart = currentStart
			longestEnd = lastPoint.Timestamp
			longestStartPct = currentStartPct
			longestEndPct = lastPoint.Capacity
		}
	}

	// We only report overnight if the inactive session is at least 4 hours
	if isSufficientOvernightDuration(maxDuration) {
		reportType := "Estimated Overnight Drain (Screen-off Fallback)"
		return &OvernightReport{
			StartTime:  longestStart,
			EndTime:    longestEnd,
			StartPct:   longestStartPct,
			EndPct:     longestEndPct,
			Drain:      longestEndPct - longestStartPct,
			Duration:   maxDuration,
			Type:       reportType,
			Provenance: "estimated",
		}, nil
	}

	return nil, fmt.Errorf("no sleep event or long idle session found in lookback window")
}

func isSufficientOvernightDuration(duration time.Duration) bool {
	return duration >= 4*time.Hour
}

// Check if the gap overlaps with night hours (8 PM to 8 AM local time)
func overlapsWithNight(start, end time.Time) bool {
	sLocal := start.Local()
	eLocal := end.Local()
	for t := sLocal; t.Before(eLocal) || t.Equal(eLocal); t = t.Add(1 * time.Hour) {
		h := t.Hour()
		if h >= 20 || h < 8 {
			return true
		}
	}
	hEnd := eLocal.Hour()
	if hEnd >= 20 || hEnd < 8 {
		return true
	}
	return false
}

// getClosestCapacity queries the database for the capacity closest to targetTime.
func getClosestCapacity(database *db.DB, targetTime time.Time) (float64, error) {
	// Look up to 10 minutes before or after target time
	start := targetTime.Add(-10 * time.Minute)
	end := targetTime.Add(10 * time.Minute)

	points, err := database.GetTelemetryRange(start, end)
	if err != nil {
		return 0, err
	}
	if len(points) == 0 {
		// Fallback: try extending lookback window to 30 minutes
		points, err = database.GetTelemetryRange(targetTime.Add(-30*time.Minute), targetTime.Add(30*time.Minute))
		if err != nil || len(points) == 0 {
			return 0, fmt.Errorf("no telemetry points near %s", targetTime)
		}
	}

	// Find the point closest in time
	closest := points[0]
	minDiff := math.Abs(float64(closest.Timestamp.Sub(targetTime)))
	for _, p := range points[1:] {
		diff := math.Abs(float64(p.Timestamp.Sub(targetTime)))
		if diff < minDiff {
			minDiff = diff
			closest = p
		}
	}

	return closest.Capacity, nil
}
