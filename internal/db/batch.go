package db

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"bati/internal/model"
)

// Batcher handles memory-buffering of telemetry to optimize disk writes (fsync).
type Batcher struct {
	db          *DB
	maxSize     int
	maxInterval time.Duration
	OnError     func(error) // Optional callback for asynchronous background errors

	mu        sync.Mutex
	buffer    []model.Telemetry
	timer     *time.Timer
	closed    bool
	closeOnce sync.Once
}

// NewBatcher initializes a Batcher with a target buffer size and flush interval.
func NewBatcher(db *DB, maxSize int, maxInterval time.Duration) *Batcher {
	b := &Batcher{
		db:          db,
		maxSize:     maxSize,
		maxInterval: maxInterval,
		buffer:      make([]model.Telemetry, 0, maxSize),
	}
	return b
}

// AddTelemetry buffers a single telemetry entry. Flushes automatically if maxSize is reached.
func (b *Batcher) AddTelemetry(p model.Telemetry) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("batcher is closed")
	}
	b.buffer = append(b.buffer, p)

	// If this is the first item in the buffer, start the timer
	if len(b.buffer) == 1 {
		b.startTimer()
	}

	shouldFlush := len(b.buffer) >= b.maxSize
	b.mu.Unlock()

	if shouldFlush {
		return b.Flush()
	}
	return nil
}

// SaveEvent saves an event and flushes any buffered telemetry first
// inside a single database transaction to guarantee consistency.
func (b *Batcher) SaveEvent(e model.Event) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("batcher is closed")
	}

	b.stopTimerActive()

	// Copy telemetry data to save transactionally alongside the event
	temp := make([]model.Telemetry, len(b.buffer))
	copy(temp, b.buffer)
	b.buffer = b.buffer[:0]
	b.mu.Unlock()

	// Write event and telemetry in a single transaction (outside mutex lock)
	err := b.db.SaveEventWithTelemetry(e, temp)
	if err != nil {
		// Restore points to buffer on failure to prevent silent data loss
		b.mu.Lock()
		if !b.closed {
			b.buffer = append(temp, b.buffer...)
		}
		b.mu.Unlock()
		return fmt.Errorf("transactional save event: %w", err)
	}

	return nil
}

// SaveDevice saves device info immediately to DB.
func (b *Batcher) SaveDevice(d model.Device) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("batcher is closed")
	}
	b.mu.Unlock()

	// Write to database (outside mutex lock)
	return b.db.SaveDevice(d)
}

// Flush writes all currently buffered telemetry records to the database.
func (b *Batcher) Flush() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return errors.New("batcher is closed")
	}

	if len(b.buffer) == 0 {
		b.mu.Unlock()
		return nil
	}

	b.stopTimerActive()

	// Copy telemetry data to save outside of the mutex lock
	temp := make([]model.Telemetry, len(b.buffer))
	copy(temp, b.buffer)
	b.buffer = b.buffer[:0] // Clear buffer
	b.mu.Unlock()

	// Write to DB outside of the lock
	err := b.db.SaveTelemetryBatch(temp)
	if err != nil {
		// Restore points to buffer on failure to prevent silent data loss
		b.mu.Lock()
		if !b.closed {
			b.buffer = append(temp, b.buffer...)
		}
		b.mu.Unlock()
		return fmt.Errorf("save telemetry batch: %w", err)
	}

	return nil
}

// Close flushes any remaining telemetry and stops background timers.
func (b *Batcher) Close() error {
	var err error
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		b.stopTimerActive()
		if len(b.buffer) == 0 {
			b.mu.Unlock()
			return
		}
		// Copy remaining telemetry to save outside of the mutex lock
		temp := make([]model.Telemetry, len(b.buffer))
		copy(temp, b.buffer)
		b.buffer = b.buffer[:0]
		b.mu.Unlock()

		err = b.db.SaveTelemetryBatch(temp)
	})
	return err
}

// startTimer starts the background timer to flush the buffer after maxInterval.
// Must be called under lock.
func (b *Batcher) startTimer() {
	b.stopTimerActive()

	b.timer = time.AfterFunc(b.maxInterval, func() {
		if err := b.Flush(); err != nil {
			b.mu.Lock()
			onError := b.OnError
			b.mu.Unlock()
			if onError != nil {
				onError(err)
			}
		}
	})
}

// stopTimerActive cancels any running timer. Must be called under lock.
func (b *Batcher) stopTimerActive() {
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
}
