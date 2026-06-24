package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"bati/internal/activity"
	batidaemon "bati/internal/daemon"
	"bati/internal/db"
	"bati/internal/logind"
	"bati/internal/model"
	"bati/internal/sysfs"
	"bati/internal/upower"

	"github.com/godbus/dbus/v5"
	_ "modernc.org/sqlite"
)

var (
	screenActive = true
	activeMutex  sync.Mutex
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
	dbFolder := filepath.Join(dataDir, "bati")
	if err := os.MkdirAll(dbFolder, 0755); err != nil {
		log.Fatalf("Error creating database directory: %v", err)
	}
	return filepath.Join(dbFolder, "bati.db")
}

func main() {
	log.Println("Starting BATI Daemon (batid)...")

	// 1. Scan for battery devices
	devs, err := sysfs.ReadBatteryDevices()
	if err != nil || len(devs) == 0 {
		log.Fatalf("No battery devices detected: %v", err)
	}
	primaryBattery := devs[0]
	log.Printf("Monitoring primary battery: %s (%s %s)", primaryBattery.ID, primaryBattery.Vendor, primaryBattery.Model)

	// 2. Open DB and init schema
	dbPath := getDbPath()
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	if err := database.InitSchema(); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Save/update device details
	if err := database.SaveDevice(primaryBattery); err != nil {
		log.Printf("Warning: failed to save device metadata: %v", err)
	}

	// 3. Set up batcher (flush every 10 points or 5 minutes)
	batcher := db.NewBatcher(database, 10, 5*time.Minute)
	batcher.OnError = func(err error) {
		log.Printf("Background database flush failed: %v", err)
	}
	defer func() {
		if err := batcher.Close(); err != nil {
			log.Printf("Error closing batcher: %v", err)
		}
	}()
	recorder := batidaemon.Recorder{
		DeviceID:    primaryBattery.ID,
		Reader:      batidaemon.TelemetryReaderFunc(sysfs.ReadTelemetry),
		Batcher:     batcher,
		ScreenState: currentScreenActive,
	}

	// Record boot event (in UTC)
	if err := batcher.SaveEvent(model.Event{
		Timestamp: time.Now().UTC(),
		Type:      "boot",
		Payload:   fmt.Sprintf("Primary battery: %s", primaryBattery.ID),
	}); err != nil {
		log.Printf("Error logging boot event: %v", err)
	}

	// 4. Initialize D-Bus connections
	sysConn, err := dbus.SystemBus()
	if err != nil {
		log.Fatalf("Failed to connect to D-Bus System Bus: %v", err)
	}
	defer sysConn.Close()

	sessConn, err := dbus.SessionBus()
	var sessConnAvailable = true
	if err != nil {
		log.Printf("Warning: failed to connect to D-Bus Session Bus: %v. Screen activity tracking might be limited.", err)
		sessConnAvailable = false
	} else {
		defer sessConn.Close()
	}

	// Context for background loops
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Match rule tracker for clean unsubscription on teardown
	var sysMatchRules []string
	var sessMatchRules []string

	// 5. Subscribe to signals
	// A. systemd-logind Sleep/Resume (System Bus)
	sleepChan, err := logind.SubscribeSleepSignals(sysConn)
	if err != nil {
		log.Printf("Warning: failed to subscribe to sleep signals: %v", err)
	} else {
		sysMatchRules = append(sysMatchRules, "type='signal',sender='org.freedesktop.login1',path='/org/freedesktop/login1',interface='org.freedesktop.login1.Manager',member='PrepareForSleep'")
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("Listening for system sleep/resume events...")
			for {
				select {
				case sig := <-sleepChan:
					if sig == nil {
						return
					}
					if sig.Name != "org.freedesktop.login1.Manager.PrepareForSleep" {
						continue
					}
					isEnteringSleep, err := logind.DecodePrepareForSleep(sig)
					if err != nil {
						log.Printf("Error decoding sleep signal: %v", err)
						continue
					}

					if isEnteringSleep {
						log.Println("System is preparing to sleep. Flushing database buffers...")
						if err := batcher.SaveEvent(model.Event{
							Timestamp: time.Now().UTC(),
							Type:      "sleep",
						}); err != nil {
							log.Printf("Error saving sleep event: %v", err)
						}
						// Flush is implicitly handled inside SaveEvent transactionally
					} else {
						log.Println("System woke up from sleep.")
						if err := batcher.SaveEvent(model.Event{
							Timestamp: time.Now().UTC(),
							Type:      "resume",
						}); err != nil {
							log.Printf("Error saving resume event: %v", err)
						}
						// Take an immediate reading post-resume
						if err := recorder.RecordAndFlush("resume"); err != nil {
							log.Printf("Error recording resume telemetry: %v", err)
						}
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// B. UPower property changes (System Bus)
	upowerChan, err := upower.SubscribePropertiesChanged(sysConn)
	if err != nil {
		log.Printf("Warning: failed to subscribe to UPower property changes: %v", err)
	} else {
		sysMatchRules = append(sysMatchRules, "type='signal',sender='org.freedesktop.UPower',member='PropertiesChanged'")
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Println("Listening for UPower status changes...")
			for {
				select {
				case sig := <-upowerChan:
					if sig == nil {
						return
					}
					if sig.Name != "org.freedesktop.DBus.Properties.PropertiesChanged" {
						continue
					}
					// Wake up and record telemetry on status change
					if err := recorder.RecordAndFlush("upower-change"); err != nil {
						log.Printf("Error recording UPower telemetry: %v", err)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// C. Session Screensaver Lock/Unlock (Session Bus)
	var activityChan chan *dbus.Signal
	if sessConnAvailable {
		var err error
		activityChan, err = activity.SubscribeToActivitySignals(sessConn)
		if err != nil {
			log.Printf("Warning: failed to subscribe to screensaver signals: %v", err)
		} else {
			sessMatchRules = append(sessMatchRules,
				"type='signal',sender='org.gnome.ScreenSaver',interface='org.gnome.ScreenSaver',member='ActiveChanged'",
				"type='signal',sender='org.freedesktop.ScreenSaver',interface='org.freedesktop.ScreenSaver',member='ActiveChanged'",
			)
			wg.Add(1)
			go func() {
				defer wg.Done()
				log.Println("Listening for user screen lock/unlock activity events...")
				for {
					select {
					case sig := <-activityChan:
						if sig == nil {
							return
						}
						screensaverActive, ok := activity.ParseActiveChanged(sig)
						if ok {
							activeMutex.Lock()
							screenActive = !screensaverActive
							activeMutex.Unlock()

							statusStr := "active"
							eventVal := "screen_on"
							if screensaverActive {
								statusStr = "inactive"
								eventVal = "screen_off"
							}
							log.Printf("User screen activity changed to: %s", statusStr)

							// Record event (outside of any mutex lock)
							if err := batcher.SaveEvent(model.Event{
								Timestamp: time.Now().UTC(),
								Type:      eventVal,
							}); err != nil {
								log.Printf("Error saving screen event: %v", err)
							}

							if err := recorder.RecordAndFlush(eventVal); err != nil {
								log.Printf("Error recording screen activity telemetry: %v", err)
							}
						}
					case <-ctx.Done():
						return
					}
				}
			}()
		}
	}

	// D. Periodic fallback timer (every 5 minutes) to ensure data points when system is static
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		// Record initial sample
		if err := recorder.RecordAndFlush("startup"); err != nil {
			log.Printf("Error recording startup telemetry: %v", err)
		}

		for {
			select {
			case <-ticker.C:
				if err := recorder.RecordBuffered("periodic"); err != nil {
					log.Printf("Error recording periodic telemetry: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// 6. Handle OS interrupts for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received signal %v. Initiating graceful shutdown...", sig)

	// Stop receiving signal notifications
	signal.Stop(sigChan)

	// Cancel contexts and wait for background routines
	cancel()

	if err := batcher.SaveEvent(model.Event{
		Timestamp: time.Now().UTC(),
		Type:      "shutdown",
	}); err != nil {
		log.Printf("Error logging shutdown event: %v", err)
	}

	// Unregister D-Bus match rules and channels to clean up resources cleanly
	for _, rule := range sysMatchRules {
		_ = sysConn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, rule)
	}
	if sleepChan != nil {
		sysConn.RemoveSignal(sleepChan)
	}
	if upowerChan != nil {
		sysConn.RemoveSignal(upowerChan)
	}
	if sessConnAvailable {
		for _, rule := range sessMatchRules {
			_ = sessConn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, rule)
		}
		if activityChan != nil {
			sessConn.RemoveSignal(activityChan)
		}
	}

	// Close connections and batcher
	if err := batcher.Close(); err != nil {
		log.Printf("Error closing batcher: %v", err)
	}
	wg.Wait()
	log.Println("BATI Daemon stopped successfully.")
}

func currentScreenActive() bool {
	activeMutex.Lock()
	defer activeMutex.Unlock()
	return screenActive
}
