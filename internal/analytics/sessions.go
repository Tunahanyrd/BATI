package analytics

import (
	"fmt"
	"math"
	"sort"
	"time"

	"bati/internal/db"
	"bati/internal/model"
)

// Session represents a segmented block of battery state.
type Session = model.Session

// DailySummary aggregates metrics for a single calendar day or rolling range.
type DailySummary = model.DailySummary

type sleepInterval struct {
	start time.Time
	end   time.Time
}

// GenerateSessions parses raw telemetry and events to build a list of sequential sessions.
func GenerateSessions(database *db.DB, start, end time.Time) ([]Session, error) {
	telemetry, err := database.GetTelemetryRange(start, end)
	if err != nil {
		return nil, fmt.Errorf("fetch telemetry: %w", err)
	}

	events, err := database.GetEventsRange(start, end)
	if err != nil {
		return nil, fmt.Errorf("fetch events: %w", err)
	}

	if len(telemetry) == 0 {
		return nil, nil
	}

	var sessions []Session

	// 1. Build a map of sleep intervals
	var sleepIntervals []sleepInterval

	// Look back in the database to see if the machine went to sleep BEFORE start
	var activeSleep *time.Time
	lastEvent, err := database.GetLastSleepResumeEventBefore(start)
	if err == nil {
		if lastEvent.Type == "sleep" {
			t := lastEvent.Timestamp
			activeSleep = &t
		}
	}

	for _, e := range events {
		if e.Type == "sleep" {
			t := e.Timestamp
			activeSleep = &t
		} else if e.Type == "resume" && activeSleep != nil {
			intervalStart := maxTime(*activeSleep, start)
			intervalEnd := minTime(e.Timestamp, end)
			if intervalEnd.After(intervalStart) {
				sleepIntervals = append(sleepIntervals, sleepInterval{start: intervalStart, end: intervalEnd})
			}
			activeSleep = nil
		}
	}
	// If currently sleeping at the end of window
	if activeSleep != nil {
		intervalStart := maxTime(*activeSleep, start)
		if end.After(intervalStart) {
			sleepIntervals = append(sleepIntervals, sleepInterval{start: intervalStart, end: end})
		}
	}
	offlineIntervals := buildOfflineIntervals(database, events, start, end)

	// Helper to check if a timestamp falls in a sleep interval (exclusive of end time)
	inSleep := func(t time.Time) (bool, time.Time, time.Time) {
		for _, intr := range sleepIntervals {
			if (t.Equal(intr.start) || t.After(intr.start)) && t.Before(intr.end) {
				return true, intr.start, intr.end
			}
		}
		return false, time.Time{}, time.Time{}
	}

	// 2. Scan telemetry and group into discharging/charging sessions, inserting sleep sessions as they occur
	var currentSession *Session

	for i := 0; i < len(telemetry); i++ {
		tPoint := telemetry[i]

		isSleeping, sleepStart, sleepEnd := inSleep(tPoint.Timestamp)
		if isSleeping {
			// If we had a running session, close it at sleep start
			if currentSession != nil {
				currentSession.EndTime = sleepStart
				currentSession.Duration = currentSession.EndTime.Sub(currentSession.StartTime)
				// The capacity at sleep start is the last telemetry point before sleep
				var endPct float64
				if i > 0 {
					endPct = telemetry[i-1].Capacity
				} else {
					lastT, err := database.GetLastTelemetryBefore(sleepStart)
					if err == nil {
						endPct = lastT.Capacity
					} else {
						endPct = tPoint.Capacity // absolute fallback
					}
				}
				currentSession.EndPct = endPct
				currentSession.DeltaPct = currentSession.EndPct - currentSession.StartPct
				if currentSession.Duration > 5*time.Second {
					sessions = append(sessions, *currentSession)
				}
				currentSession = nil
			}

			// Add the sleeping session
			var startPct float64
			if i > 0 {
				startPct = telemetry[i-1].Capacity
			} else {
				lastT, err := database.GetLastTelemetryBefore(sleepStart)
				if err == nil {
					startPct = lastT.Capacity
				} else {
					startPct = tPoint.Capacity
				}
			}

			// Fast-forward index past the sleep interval (up to the resume time)
			for i < len(telemetry) && telemetry[i].Timestamp.Before(sleepEnd) {
				i++
			}

			var endPct float64
			if i < len(telemetry) {
				endPct = telemetry[i].Capacity
			} else {
				// Sleep ended after telemetry window
				lastT, err := database.GetLastTelemetryBefore(sleepEnd)
				if err == nil {
					endPct = lastT.Capacity
				} else {
					endPct = startPct
				}
			}

			sessions = append(sessions, Session{
				StartTime:  sleepStart,
				EndTime:    sleepEnd,
				Type:       "sleeping",
				StartPct:   startPct,
				EndPct:     endPct,
				DeltaPct:   endPct - startPct,
				Duration:   sleepEnd.Sub(sleepStart),
				Provenance: "observed",
			})

			i-- // Adjust for loop increment
			continue
		}

		// Map UPower/sysfs status to specific session types
		sessionType := "discharging"
		switch tPoint.Status {
		case "Charging":
			sessionType = "charging"
		case "Full":
			sessionType = "full"
		case "Not charging":
			sessionType = "not_charging"
		case "Discharging":
			sessionType = "discharging"
		default:
			// Fallback based on energy rate if UPower status is Unknown/empty
			if tPoint.EnergyRate < 0 {
				sessionType = "charging"
			} else if tPoint.EnergyRate == 0 {
				if tPoint.Capacity >= 99.0 {
					sessionType = "full"
				} else {
					sessionType = "not_charging"
				}
			}
		}

		if currentSession == nil {
			currentSession = &Session{
				StartTime:  tPoint.Timestamp,
				Type:       sessionType,
				StartPct:   tPoint.Capacity,
				Provenance: "observed",
			}
		} else if currentSession.Type != sessionType {
			// Status flipped, close current and start new
			currentSession.EndTime = tPoint.Timestamp
			currentSession.Duration = currentSession.EndTime.Sub(currentSession.StartTime)
			currentSession.EndPct = tPoint.Capacity
			currentSession.DeltaPct = currentSession.EndPct - currentSession.StartPct
			if currentSession.Duration > 5*time.Second {
				sessions = append(sessions, *currentSession)
			}

			currentSession = &Session{
				StartTime:  tPoint.Timestamp,
				Type:       sessionType,
				StartPct:   tPoint.Capacity,
				Provenance: "observed",
			}
		} else {
			// Downgrade provenance to "inferred" if there's a gap > 15 mins
			if i > 0 && tPoint.Timestamp.Sub(telemetry[i-1].Timestamp) > 15*time.Minute {
				currentSession.Provenance = "inferred"
			}
		}
	}

	// Close any remaining active session at the end of telemetry
	if currentSession != nil {
		lastPoint := telemetry[len(telemetry)-1]
		currentSession.EndTime = lastPoint.Timestamp
		currentSession.Duration = currentSession.EndTime.Sub(currentSession.StartTime)
		currentSession.EndPct = lastPoint.Capacity
		currentSession.DeltaPct = currentSession.EndPct - currentSession.StartPct
		if currentSession.Duration > 5*time.Second {
			sessions = append(sessions, *currentSession)
		}
	}

	sessions = ensureSleepSessions(database, sessions, sleepIntervals)
	return removeOfflineIntervals(database, sessions, offlineIntervals), nil
}

func buildOfflineIntervals(
	database *db.DB,
	events []model.Event,
	start, end time.Time,
) []sleepInterval {
	var intervals []sleepInterval
	var activeShutdown *time.Time

	if lastEvent, err := database.GetLastEventBefore(start); err == nil && lastEvent.Type == "shutdown" {
		t := lastEvent.Timestamp
		activeShutdown = &t
	}

	for _, e := range events {
		switch e.Type {
		case "shutdown":
			t := e.Timestamp
			activeShutdown = &t
		case "boot":
			if activeShutdown == nil {
				continue
			}
			intervalStart := maxTime(*activeShutdown, start)
			intervalEnd := minTime(e.Timestamp, end)
			if intervalEnd.After(intervalStart) {
				intervals = append(intervals, sleepInterval{start: intervalStart, end: intervalEnd})
			}
			activeShutdown = nil
		}
	}

	if activeShutdown != nil {
		intervalStart := maxTime(*activeShutdown, start)
		if end.After(intervalStart) {
			intervals = append(intervals, sleepInterval{start: intervalStart, end: end})
		}
	}

	return intervals
}

func removeOfflineIntervals(
	database *db.DB,
	sessions []Session,
	intervals []sleepInterval,
) []Session {
	for _, interval := range intervals {
		updated := make([]Session, 0, len(sessions)+1)
		for _, session := range sessions {
			if !session.EndTime.After(interval.start) || !session.StartTime.Before(interval.end) {
				updated = append(updated, session)
				continue
			}

			if session.StartTime.Before(interval.start) {
				before := session
				beforeEnd, beforePct := sessionBoundaryBefore(database, interval.start, session.StartTime, session.StartPct)
				before.EndTime = beforeEnd
				before.EndPct = beforePct
				before.Duration = before.EndTime.Sub(before.StartTime)
				before.DeltaPct = before.EndPct - before.StartPct
				if before.Duration > 5*time.Second {
					updated = append(updated, before)
				}
			}

			if session.EndTime.After(interval.end) {
				after := session
				afterStart, afterPct := sessionBoundaryAfter(database, interval.end, session.EndTime, session.EndPct)
				after.StartTime = afterStart
				after.StartPct = afterPct
				after.Duration = after.EndTime.Sub(after.StartTime)
				after.DeltaPct = after.EndPct - after.StartPct
				if after.Duration > 5*time.Second {
					updated = append(updated, after)
				}
			}
		}
		sessions = updated
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.Before(sessions[j].StartTime)
	})
	return sessions
}

func sessionBoundaryBefore(database *db.DB, target, lowerBound time.Time, fallbackPct float64) (time.Time, float64) {
	point, err := database.GetLastTelemetryBefore(target)
	if err == nil && !point.Timestamp.Before(lowerBound) && !point.Timestamp.After(target) {
		return point.Timestamp, point.Capacity
	}
	return target, fallbackPct
}

func sessionBoundaryAfter(database *db.DB, target, upperBound time.Time, fallbackPct float64) (time.Time, float64) {
	point, err := database.GetFirstTelemetryAfter(target)
	if err == nil && !point.Timestamp.Before(target) && !point.Timestamp.After(upperBound) {
		return point.Timestamp, point.Capacity
	}
	return target, fallbackPct
}

func ensureSleepSessions(
	database *db.DB,
	sessions []Session,
	intervals []sleepInterval,
) []Session {
	for _, interval := range intervals {
		represented := false
		for _, session := range sessions {
			if session.Type == "sleeping" &&
				session.StartTime.Equal(interval.start) &&
				session.EndTime.Equal(interval.end) {
				represented = true
				break
			}
		}
		if represented {
			continue
		}

		startPct, startOK := closestBoundaryCapacity(database, interval.start)
		endPct, endOK := closestBoundaryCapacity(database, interval.end)
		if !startOK && !endOK {
			continue
		}
		if !startOK {
			startPct = endPct
		}
		if !endOK {
			endPct = startPct
		}

		updated := make([]Session, 0, len(sessions)+2)
		for _, session := range sessions {
			if session.Type == "sleeping" ||
				!session.EndTime.After(interval.start) ||
				!session.StartTime.Before(interval.end) {
				updated = append(updated, session)
				continue
			}
			if session.StartTime.Before(interval.start) {
				before := session
				before.EndTime = interval.start
				before.EndPct = startPct
				before.Duration = before.EndTime.Sub(before.StartTime)
				before.DeltaPct = before.EndPct - before.StartPct
				if before.Duration > 5*time.Second {
					updated = append(updated, before)
				}
			}
			if session.EndTime.After(interval.end) {
				after := session
				after.StartTime = interval.end
				after.StartPct = endPct
				after.Duration = after.EndTime.Sub(after.StartTime)
				after.DeltaPct = after.EndPct - after.StartPct
				if after.Duration > 5*time.Second {
					updated = append(updated, after)
				}
			}
		}
		updated = append(updated, Session{
			StartTime:  interval.start,
			EndTime:    interval.end,
			Type:       "sleeping",
			StartPct:   startPct,
			EndPct:     endPct,
			DeltaPct:   endPct - startPct,
			Duration:   interval.end.Sub(interval.start),
			Provenance: "observed",
		})
		sessions = updated
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.Before(sessions[j].StartTime)
	})
	return sessions
}

func closestBoundaryCapacity(database *db.DB, target time.Time) (float64, bool) {
	before, beforeErr := database.GetLastTelemetryBefore(target)
	after, afterErr := database.GetFirstTelemetryAfter(target)
	switch {
	case beforeErr == nil && afterErr == nil:
		if target.Sub(before.Timestamp) <= after.Timestamp.Sub(target) {
			return before.Capacity, true
		}
		return after.Capacity, true
	case beforeErr == nil:
		return before.Capacity, true
	case afterErr == nil:
		return after.Capacity, true
	default:
		return 0, false
	}
}

// GenerateRangeSummary computes battery and screen statistics over any custom time range.
func GenerateRangeSummary(database *db.DB, start, end time.Time) (*DailySummary, error) {
	sessions, err := GenerateSessions(database, start, end)
	if err != nil {
		return nil, err
	}

	telemetry, err := database.GetTelemetryRange(start, end)
	if err != nil {
		return nil, err
	}

	summary := &DailySummary{
		Date:       start,
		Sessions:   sessions,
		Provenance: "observed",
	}

	// 1. Sum up charge/discharge deltas and sleep duration from sessions
	for _, s := range sessions {
		if s.Type == "sleeping" {
			summary.SleepDuration += s.Duration
			if s.DeltaPct < 0 {
				summary.TotalDischarge += math.Abs(s.DeltaPct)
			} else {
				summary.TotalCharge += s.DeltaPct
			}
		} else if s.Type == "discharging" {
			if s.DeltaPct < 0 {
				summary.TotalDischarge += math.Abs(s.DeltaPct)
			}
		} else if s.Type == "charging" {
			if s.DeltaPct > 0 {
				summary.TotalCharge += s.DeltaPct
			}
		}
	}

	// 2. Sum up screen-on (active) active time from telemetry points
	if len(telemetry) > 0 {
		var activeDuration time.Duration
		defaultInterval := 5 * time.Minute

		for i := 0; i < len(telemetry); i++ {
			if telemetry[i].ScreenOn {
				var duration time.Duration
				if i < len(telemetry)-1 {
					duration = telemetry[i+1].Timestamp.Sub(telemetry[i].Timestamp)
					if duration > 10*time.Minute {
						duration = defaultInterval
					}
				} else {
					duration = defaultInterval
				}
				if telemetry[i].Timestamp.Add(duration).After(end) {
					duration = max(0, end.Sub(telemetry[i].Timestamp))
				}
				activeDuration += duration
			}
		}
		summary.ActiveDuration = activeDuration
	}

	return summary, nil
}

// GenerateDailySummary computes aggregate data for a specific 24h calendar day.
func GenerateDailySummary(database *db.DB, targetDay time.Time) (*DailySummary, error) {
	// Boundary for the target day
	start := time.Date(targetDay.Year(), targetDay.Month(), targetDay.Day(), 0, 0, 0, 0, targetDay.Location())
	end := start.AddDate(0, 0, 1)
	return GenerateRangeSummary(database, start, end)
}
