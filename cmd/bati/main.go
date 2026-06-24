package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime/debug"

	"bati/internal/db"
	"bati/internal/gui"

	"gioui.org/app"
	_ "modernc.org/sqlite"
)

const defaultGUIMemoryLimit = 96 * 1024 * 1024

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
	configureRuntime()

	dbPath := getDbPath()
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	if err := database.InitSchema(); err != nil {
		database.Close()
		log.Fatalf("Failed to initialize database: %v", err)
	}

	go func() {
		defer database.Close()
		if err := gui.Run(database, dbPath); err != nil {
			log.Fatalf("GUI execution error: %v", err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func configureRuntime() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(75)
	}
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(defaultGUIMemoryLimit)
	}
}
