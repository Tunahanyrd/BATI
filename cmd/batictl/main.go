package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bati/internal/analytics"
	"bati/internal/db"
	"bati/internal/diagnostics"

	_ "modernc.org/sqlite"
)

func getDbPath() string {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Error determining home directory: %v", err)
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataDir, "bati", "bati.db")
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "BATI (Battery Analytics & Timeline Interface) CLI\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  batictl <command> [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  status       Display daemon database and recording status\n")
		fmt.Fprintf(os.Stderr, "  samples      List telemetry data points (use --today)\n")
		fmt.Fprintf(os.Stderr, "  overnight    Calculate and display latest overnight battery drain\n")
		fmt.Fprintf(os.Stderr, "  activity     List screen activity periods (use --today)\n")
		fmt.Fprintf(os.Stderr, "  report       Show daily battery use sessions and active time summaries (use --today)\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	// Custom argument parsing to allow flags anywhere on the command line
	todayOnly := false
	var cmd string
	for _, arg := range os.Args[1:] {
		if arg == "--today" || arg == "-today" {
			todayOnly = true
		} else if !strings.HasPrefix(arg, "-") && cmd == "" {
			cmd = arg
		}
	}

	if cmd == "" {
		flag.Usage()
		os.Exit(1)
	}
	dbPath := getDbPath()

	var database *db.DB
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if cmd != "status" {
			fmt.Printf("Database file does not exist yet at %s.\nIs the daemon running?\n", dbPath)
			os.Exit(1)
		}
	} else {
		var err error
		database, err = db.Open(dbPath)
		if err != nil {
			log.Fatalf("Error opening database: %v", err)
		}
		defer database.Close()
	}

	switch cmd {
	case "status":
		runStatus(dbPath, database)
	case "samples":
		runSamples(database, todayOnly)
	case "overnight":
		runOvernight(database)
	case "activity":
		runActivity(database, todayOnly)
	case "report":
		runReport(database, todayOnly)
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		flag.Usage()
		os.Exit(1)
	}
}

func runStatus(dbPath string, database *db.DB) {
	status := diagnostics.Snapshot(database, dbPath, time.Now().UTC(), diagnostics.SystemdUserChecker{
		Unit:    "batid.service",
		Timeout: 2 * time.Second,
	})

	fmt.Printf("BATI Status\n")
	fmt.Printf("-----------\n")
	fmt.Printf("Service:       %s\n", status.Service.Summary())
	if status.Service.Err != "" {
		fmt.Printf("Service note:  %s\n", status.Service.Err)
	}
	fmt.Printf("DB Path:       %s\n", status.DBPath)
	if !status.DBExists {
		fmt.Printf("DB File:       Not created yet\n")
	} else {
		fmt.Printf("DB Size:       %.2f MB\n", float64(status.DBSizeBytes)/(1024*1024))
	}
	if status.DBErr != "" {
		fmt.Printf("DB Error:      %s\n", status.DBErr)
	}

	if status.LatestSampleAvailable {
		staleText := ""
		if status.LatestSampleStale {
			staleText = " stale"
		}
		fmt.Printf("Latest sample: %s (%s ago%s)\n",
			status.LatestSampleTime.Local().Format("2006-01-02 15:04:05"),
			formatStatusAge(status.LatestSampleAge),
			staleText,
		)
	} else {
		fmt.Printf("Latest sample: none\n")
	}

	if status.TodaySamplesErr == "" {
		fmt.Printf("Today samples: %d points\n", status.TodaySampleCount)
	} else {
		fmt.Printf("Today samples: Error reading telemetry: %s\n", status.TodaySamplesErr)
	}
	if status.TodayEventsErr == "" {
		fmt.Printf("Today events:  %d events\n", status.TodayEventCount)
	} else {
		fmt.Printf("Today events:  Error reading events: %s\n", status.TodayEventsErr)
	}
	fmt.Printf("Live battery:  %s\n", status.Live.Summary())
	fmt.Printf("Action:        %s\n", status.Recommendation)
}

func formatStatusAge(age time.Duration) string {
	if age < 0 {
		age = 0
	}
	switch {
	case age < 5*time.Second:
		return "just now"
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age/time.Second))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
}

func runSamples(database *db.DB, todayOnly bool) {
	var start, end time.Time
	now := time.Now().UTC()

	if todayOnly {
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		end = now
	} else {
		start = now.Add(-24 * time.Hour)
		end = now
	}

	points, err := database.GetTelemetryRange(start, end)
	if err != nil {
		log.Fatalf("Error reading telemetry range: %v", err)
	}

	if len(points) == 0 {
		fmt.Println("No battery telemetry samples found in the requested range.")
		return
	}

	fmt.Printf("%-20s %-8s %-12s %-10s %-8s %-10s\n", "Timestamp", "Capacity", "Status", "EnergyRate", "Voltage", "ScreenActive")
	fmt.Println(strings.Repeat("-", 75))
	for _, p := range points {
		screenStr := "no"
		if p.ScreenOn {
			screenStr = "yes"
		}
		// Convert UTC timestamp to Local for display
		fmt.Printf("%-20s %-8.1f%% %-12s %-10.2fW %-8.2fV %-10s\n",
			p.Timestamp.Local().Format("2006-01-02 15:04:05"),
			p.Capacity,
			p.Status,
			p.EnergyRate,
			p.Voltage,
			screenStr,
		)
	}
}

func runOvernight(database *db.DB) {
	report, err := analytics.CalculateOvernightDrain(database)
	if err != nil {
		fmt.Printf("Could not calculate overnight drain: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s\n", strings.ToLower(report.Type))
	// Convert UTC start/end to Local for display
	fmt.Printf("%s -> %s\n", report.StartTime.Local().Format("02 Jan 15:04"), report.EndTime.Local().Format("02 Jan 15:04"))
	fmt.Printf("%.0f%% -> %.0f%%\n", report.StartPct, report.EndPct)
	fmt.Printf("%.1f%% over %s\n", report.Drain, report.Duration.Round(time.Minute))
	fmt.Printf("confidence: %s\n", report.Provenance)
}

func runActivity(database *db.DB, todayOnly bool) {
	var start, end time.Time
	now := time.Now().UTC()

	if todayOnly {
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		end = now
	} else {
		start = now.Add(-24 * time.Hour)
		end = now
	}

	events, err := database.GetEventsRange(start, end)
	if err != nil {
		log.Fatalf("Error reading events: %v", err)
	}

	if len(events) == 0 {
		fmt.Println("No activity events found in the requested range.")
		return
	}

	fmt.Printf("%-20s %-15s %-40s\n", "Timestamp", "Event Type", "Payload")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range events {
		// Convert UTC to Local for display
		fmt.Printf("%-20s %-15s %-40s\n",
			e.Timestamp.Local().Format("2006-01-02 15:04:05"),
			e.Type,
			e.Payload,
		)
	}
}

func runReport(database *db.DB, todayOnly bool) {
	now := time.Now().UTC()
	var summary *analytics.DailySummary
	var err error

	if todayOnly {
		summary, err = analytics.GenerateDailySummary(database, time.Now())
		if err != nil {
			log.Fatalf("Error generating report: %v", err)
		}
		fmt.Printf("BATI Daily Battery Report (Today - %s)\n", summary.Date.Local().Format("02 Jan 2006"))
	} else {
		start := now.Add(-24 * time.Hour)
		summary, err = analytics.GenerateRangeSummary(database, start, now)
		if err != nil {
			log.Fatalf("Error generating report: %v", err)
		}
		fmt.Printf("BATI Battery Report (Last 24h: %s -> %s)\n", start.Local().Format("02 Jan 15:04"), now.Local().Format("02 Jan 15:04"))
	}

	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Battery Discharged:  %.1f%%\n", summary.TotalDischarge)
	fmt.Printf("Battery Charged:     %.1f%%\n", summary.TotalCharge)
	fmt.Printf("Sleep Duration:      %s\n", summary.SleepDuration.Round(time.Minute))
	fmt.Printf("Active Screen Time:  %s\n", summary.ActiveDuration.Round(time.Minute))
	fmt.Println()

	if len(summary.Sessions) == 0 {
		fmt.Println("No session records found for this period.")
		return
	}

	fmt.Printf("Oturumlar (Sessions):\n")
	fmt.Printf("%-3s %-20s %-12s %-12s %-10s %-8s %-12s\n", "#", "Type", "Start", "End", "Change", "Duration", "Confidence")
	fmt.Println(strings.Repeat("-", 80))
	for i, s := range summary.Sessions {
		changeSign := ""
		if s.DeltaPct > 0 {
			changeSign = "+"
		}
		changeStr := fmt.Sprintf("%s%.1f%%", changeSign, s.DeltaPct)

		// Map session types to user friendly names
		typeName := s.Type
		switch s.Type {
		case "full":
			typeName = "full"
		case "not_charging":
			typeName = "not charging (AC)"
		}

		// Convert UTC start/end to Local for display
		fmt.Printf("%-3d %-20s %-12s %-12s %-10s %-8s %-12s\n",
			i+1,
			typeName,
			s.StartTime.Local().Format("15:04"),
			s.EndTime.Local().Format("15:04"),
			changeStr,
			s.Duration.Round(time.Minute),
			s.Provenance,
		)
	}
}
