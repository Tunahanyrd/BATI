package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"bati/internal/model"
)

// TimeFormatNano is a fixed-width UTC format preserving nanosecond precision
// to ensure timestamps are lexicographically sortable in SQLite.
const TimeFormatNano = "2006-01-02T15:04:05.000000000Z"

// DB wraps a standard sql.DB connection and provides application-specific queries.
type DB struct {
	conn *sql.DB
}

// Open initializes or opens the SQLite database at the specified file path.
func Open(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// Enable WAL mode and set a busy timeout for better concurrency and performance
	if _, err := conn.Exec("PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL; PRAGMA busy_timeout = 5000;"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set wal/timeout mode: %w", err)
	}

	// Limit connection pool to 1 connection to prevent SQLite lock contention (SQLITE_BUSY)
	conn.SetMaxOpenConns(1)

	return &DB{conn: conn}, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// InitSchema creates the database tables if they do not exist.
// It automatically detects and migrates existing tables that use timestamp as the primary key.
func (db *DB) InitSchema() error {
	// 1. Check if we need to migrate the telemetry table (if it exists but lacks the surrogate 'id' column)
	var hasTelemetryTable bool
	err := db.conn.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='telemetry'").Scan(&hasTelemetryTable)
	if err == nil && hasTelemetryTable {
		rows, err := db.conn.Query("PRAGMA table_info(telemetry)")
		if err == nil {
			hasIDColumn := false
			for rows.Next() {
				var cid int
				var name, typeStr string
				var notnull, pk int
				var dfltVal interface{}
				if err := rows.Scan(&cid, &name, &typeStr, &notnull, &dfltVal, &pk); err == nil {
					if name == "id" {
						hasIDColumn = true
					}
				}
			}
			rows.Close()

			if !hasIDColumn {
				// Run table schema migration inside a transaction
				tx, err := db.conn.Begin()
				if err != nil {
					return fmt.Errorf("begin migration tx: %w", err)
				}
				defer tx.Rollback()

				// Rename old table
				if _, err := tx.Exec("ALTER TABLE telemetry RENAME TO telemetry_old"); err != nil {
					return fmt.Errorf("rename telemetry table: %w", err)
				}

				// Create new table with surrogate PK and UNIQUE timestamp
				newTableSchema := `
				CREATE TABLE telemetry (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					timestamp TEXT UNIQUE NOT NULL,
					capacity REAL NOT NULL,
					status TEXT NOT NULL,
					energy_rate REAL NOT NULL,
					voltage REAL NOT NULL,
					screen_on INTEGER NOT NULL
				);`
				if _, err := tx.Exec(newTableSchema); err != nil {
					return fmt.Errorf("create migrated telemetry table: %w", err)
				}

				// Copy telemetry records over
				copyDataQuery := `
				INSERT INTO telemetry (timestamp, capacity, status, energy_rate, voltage, screen_on)
				SELECT timestamp, capacity, status, energy_rate, voltage, screen_on FROM telemetry_old;`
				if _, err := tx.Exec(copyDataQuery); err != nil {
					return fmt.Errorf("copy telemetry data to new table: %w", err)
				}

				// Drop old schema
				if _, err := tx.Exec("DROP TABLE telemetry_old"); err != nil {
					return fmt.Errorf("drop old telemetry table: %w", err)
				}

				if err := tx.Commit(); err != nil {
					return fmt.Errorf("commit migration tx: %w", err)
				}
			}
		}
	}

	// 2. Initialize schema (creates tables/indices if not exist)
	schema := `
	CREATE TABLE IF NOT EXISTS devices (
		id TEXT PRIMARY KEY,
		vendor TEXT,
		model TEXT,
		serial TEXT,
		design_capacity REAL,
		full_capacity REAL,
		technology TEXT,
		cycle_count INTEGER,
		is_power_supply INTEGER,
		first_seen TEXT NOT NULL,
		last_seen TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS telemetry (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT UNIQUE NOT NULL,
		capacity REAL NOT NULL,
		status TEXT NOT NULL,
		energy_rate REAL NOT NULL,
		voltage REAL NOT NULL,
		screen_on INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		type TEXT NOT NULL,
		payload TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_telemetry_timestamp ON telemetry(timestamp);
	CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
	`
	_, err = db.conn.Exec(schema)
	if err != nil {
		return fmt.Errorf("execute schema: %w", err)
	}
	return nil
}

// SaveDevice inserts or updates a device record.
func (db *DB) SaveDevice(d model.Device) error {
	query := `
	INSERT INTO devices (
		id, vendor, model, serial, design_capacity, full_capacity,
		technology, cycle_count, is_power_supply, first_seen, last_seen
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		full_capacity=excluded.full_capacity,
		cycle_count=excluded.cycle_count,
		last_seen=excluded.last_seen;
	`
	_, err := db.conn.Exec(query,
		d.ID, d.Vendor, d.Model, d.Serial, d.DesignCapacity, d.FullCapacity,
		d.Technology, d.CycleCount, mapBoolToInt(d.IsPowerSupply),
		d.FirstSeen.UTC().Format(TimeFormatNano),
		d.LastSeen.UTC().Format(TimeFormatNano),
	)
	if err != nil {
		return fmt.Errorf("save device: %w", err)
	}
	return nil
}

// GetPrimaryDevice retrieves the first device record (usually the primary laptop battery).
func (db *DB) GetPrimaryDevice() (model.Device, error) {
	query := `
		SELECT id, vendor, model, serial, design_capacity, full_capacity,
		       technology, cycle_count, is_power_supply, first_seen, last_seen
		FROM devices
		LIMIT 1
	`
	row := db.conn.QueryRow(query)
	var d model.Device
	var isPowerSupplyInt int
	var fsStr, lsStr string
	err := row.Scan(&d.ID, &d.Vendor, &d.Model, &d.Serial, &d.DesignCapacity, &d.FullCapacity,
		&d.Technology, &d.CycleCount, &isPowerSupplyInt, &fsStr, &lsStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Device{}, err
		}
		return model.Device{}, fmt.Errorf("scan primary device: %w", err)
	}

	d.IsPowerSupply = isPowerSupplyInt != 0
	fs, err := parseTimestamp(fsStr)
	if err == nil {
		d.FirstSeen = fs
	}
	ls, err := parseTimestamp(lsStr)
	if err == nil {
		d.LastSeen = ls
	}

	return d, nil
}

// GetLastFullChargeBefore returns the latest observed full-charge telemetry.
func (db *DB) GetLastFullChargeBefore(before time.Time) (model.Telemetry, error) {
	query := `
		SELECT timestamp, capacity, status, energy_rate, voltage, screen_on
		FROM telemetry
		WHERE timestamp <= ?
		  AND capacity >= 99.5
		  AND lower(replace(status, ' ', '_')) IN ('charging', 'full')
		ORDER BY timestamp DESC
		LIMIT 1
	`
	row := db.conn.QueryRow(query, before.UTC().Format(TimeFormatNano))
	var point model.Telemetry
	var timestamp string
	var screenOn int
	if err := row.Scan(
		&timestamp,
		&point.Capacity,
		&point.Status,
		&point.EnergyRate,
		&point.Voltage,
		&screenOn,
	); err != nil {
		return model.Telemetry{}, err
	}
	parsed, err := parseTimestamp(timestamp)
	if err != nil {
		return model.Telemetry{}, fmt.Errorf("parse full charge timestamp: %w", err)
	}
	point.Timestamp = parsed
	point.ScreenOn = screenOn != 0
	return point, nil
}

// SaveTelemetryBatch inserts multiple telemetry snapshots in a single transaction.
func (db *DB) SaveTelemetryBatch(points []model.Telemetry) error {
	if len(points) == 0 {
		return nil
	}

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := db.saveTelemetryBatchTx(tx, points); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// SaveEvent inserts a single event record.
func (db *DB) SaveEvent(e model.Event) error {
	query := `INSERT INTO events (timestamp, type, payload) VALUES (?, ?, ?)`
	_, err := db.conn.Exec(query,
		e.Timestamp.UTC().Format(TimeFormatNano),
		e.Type,
		e.Payload,
	)
	if err != nil {
		return fmt.Errorf("save event: %w", err)
	}
	return nil
}

// SaveEventWithTelemetry saves an event and flushes telemetry batch inside a single transaction.
// This prevents silent data loss or inconsistent boundary state if one fails.
func (db *DB) SaveEventWithTelemetry(e model.Event, points []model.Telemetry) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Save event
	query := `INSERT INTO events (timestamp, type, payload) VALUES (?, ?, ?)`
	_, err = tx.Exec(query,
		e.Timestamp.UTC().Format(TimeFormatNano),
		e.Type,
		e.Payload,
	)
	if err != nil {
		return fmt.Errorf("save event tx: %w", err)
	}

	// 2. Save telemetry
	if len(points) > 0 {
		if err := db.saveTelemetryBatchTx(tx, points); err != nil {
			return fmt.Errorf("save telemetry tx: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// saveTelemetryBatchTx runs telemetry inserts on an existing transaction.
func (db *DB) saveTelemetryBatchTx(tx *sql.Tx, points []model.Telemetry) error {
	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO telemetry (timestamp, capacity, status, energy_rate, voltage, screen_on)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, p := range points {
		_, err := stmt.Exec(
			p.Timestamp.UTC().Format(TimeFormatNano),
			p.Capacity,
			p.Status,
			p.EnergyRate,
			p.Voltage,
			mapBoolToInt(p.ScreenOn),
		)
		if err != nil {
			return fmt.Errorf("exec telemetry stmt: %w", err)
		}
	}
	return nil
}

// GetTelemetryRange retrieves telemetry records between start and end times (inclusive).
// Timestamps are returned in UTC to prevent timezone, DST, and sorting ambiguities.
func (db *DB) GetTelemetryRange(start, end time.Time) ([]model.Telemetry, error) {
	query := `
		SELECT timestamp, capacity, status, energy_rate, voltage, screen_on
		FROM telemetry
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp ASC
	`
	rows, err := db.conn.Query(query, start.UTC().Format(TimeFormatNano), end.UTC().Format(TimeFormatNano))
	if err != nil {
		return nil, fmt.Errorf("query telemetry range: %w", err)
	}
	defer rows.Close()

	var points []model.Telemetry
	for rows.Next() {
		var p model.Telemetry
		var tsStr string
		var screenOnInt int
		if err := rows.Scan(&tsStr, &p.Capacity, &p.Status, &p.EnergyRate, &p.Voltage, &screenOnInt); err != nil {
			return nil, fmt.Errorf("scan telemetry row: %w", err)
		}
		t, err := parseTimestamp(tsStr)
		if err != nil {
			return nil, fmt.Errorf("parse telemetry timestamp: %w", err)
		}
		p.Timestamp = t
		p.ScreenOn = screenOnInt != 0
		points = append(points, p)
	}
	return points, nil
}

// GetEventsRange retrieves event records between start and end times (inclusive).
// Timestamps are returned in UTC.
func (db *DB) GetEventsRange(start, end time.Time) ([]model.Event, error) {
	query := `
		SELECT id, timestamp, type, payload
		FROM events
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp ASC
	`
	rows, err := db.conn.Query(query, start.UTC().Format(TimeFormatNano), end.UTC().Format(TimeFormatNano))
	if err != nil {
		return nil, fmt.Errorf("query events range: %w", err)
	}
	defer rows.Close()

	var events []model.Event
	for rows.Next() {
		var e model.Event
		var tsStr string
		if err := rows.Scan(&e.ID, &tsStr, &e.Type, &e.Payload); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		t, err := parseTimestamp(tsStr)
		if err != nil {
			return nil, fmt.Errorf("parse event timestamp: %w", err)
		}
		e.Timestamp = t
		events = append(events, e)
	}
	return events, nil
}

// GetLastEventBefore returns the most recent event before the target timestamp.
// Useful for reconstructing sleep states across window boundaries.
func (db *DB) GetLastEventBefore(t time.Time) (model.Event, error) {
	query := `
		SELECT id, timestamp, type, payload
		FROM events
		WHERE timestamp < ?
		ORDER BY timestamp DESC
		LIMIT 1
	`
	row := db.conn.QueryRow(query, t.UTC().Format(TimeFormatNano))
	var e model.Event
	var tsStr string
	err := row.Scan(&e.ID, &tsStr, &e.Type, &e.Payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Event{}, err
		}
		return model.Event{}, fmt.Errorf("scan last event: %w", err)
	}

	parsed, err := parseTimestamp(tsStr)
	if err != nil {
		return model.Event{}, fmt.Errorf("parse last event time: %w", err)
	}
	e.Timestamp = parsed
	return e, nil
}

// GetLastSleepResumeEventBefore returns the most recent sleep or resume event before the target timestamp.
func (db *DB) GetLastSleepResumeEventBefore(t time.Time) (model.Event, error) {
	query := `
		SELECT id, timestamp, type, payload
		FROM events
		WHERE timestamp < ? AND type IN ('sleep', 'resume')
		ORDER BY timestamp DESC
		LIMIT 1
	`
	row := db.conn.QueryRow(query, t.UTC().Format(TimeFormatNano))
	var e model.Event
	var tsStr string
	err := row.Scan(&e.ID, &tsStr, &e.Type, &e.Payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Event{}, err
		}
		return model.Event{}, fmt.Errorf("scan last sleep/resume event: %w", err)
	}

	parsed, err := parseTimestamp(tsStr)
	if err != nil {
		return model.Event{}, fmt.Errorf("parse last sleep/resume event time: %w", err)
	}
	e.Timestamp = parsed
	return e, nil
}

// GetLastTelemetryBefore returns the most recent telemetry point before or at the target timestamp.
func (db *DB) GetLastTelemetryBefore(t time.Time) (model.Telemetry, error) {
	query := `
		SELECT timestamp, capacity, status, energy_rate, voltage, screen_on
		FROM telemetry
		WHERE timestamp <= ?
		ORDER BY timestamp DESC
		LIMIT 1
	`
	row := db.conn.QueryRow(query, t.UTC().Format(TimeFormatNano))
	var p model.Telemetry
	var tsStr string
	var screenOnInt int
	err := row.Scan(&tsStr, &p.Capacity, &p.Status, &p.EnergyRate, &p.Voltage, &screenOnInt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Telemetry{}, err
		}
		return model.Telemetry{}, fmt.Errorf("scan last telemetry: %w", err)
	}

	parsed, err := parseTimestamp(tsStr)
	if err != nil {
		return model.Telemetry{}, fmt.Errorf("parse last telemetry time: %w", err)
	}
	p.Timestamp = parsed
	p.ScreenOn = screenOnInt != 0
	return p, nil
}

// GetFirstTelemetryAfter returns the first telemetry point at or after the target timestamp.
func (db *DB) GetFirstTelemetryAfter(t time.Time) (model.Telemetry, error) {
	query := `
		SELECT timestamp, capacity, status, energy_rate, voltage, screen_on
		FROM telemetry
		WHERE timestamp >= ?
		ORDER BY timestamp ASC
		LIMIT 1
	`
	row := db.conn.QueryRow(query, t.UTC().Format(TimeFormatNano))
	var p model.Telemetry
	var tsStr string
	var screenOnInt int
	err := row.Scan(&tsStr, &p.Capacity, &p.Status, &p.EnergyRate, &p.Voltage, &screenOnInt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Telemetry{}, err
		}
		return model.Telemetry{}, fmt.Errorf("scan first telemetry after: %w", err)
	}

	parsed, err := parseTimestamp(tsStr)
	if err != nil {
		return model.Telemetry{}, fmt.Errorf("parse first telemetry time: %w", err)
	}
	p.Timestamp = parsed
	p.ScreenOn = screenOnInt != 0
	return p, nil
}

// Helpers
func mapBoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTimestamp(tsStr string) (time.Time, error) {
	t, err := time.Parse(TimeFormatNano, tsStr)
	if err == nil {
		return t.UTC(), nil
	}
	t, err = time.Parse(time.RFC3339, tsStr)
	if err == nil {
		return t.UTC(), nil
	}
	t, err = time.Parse("2006-01-02 15:04:05", tsStr)
	if err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, err
}
