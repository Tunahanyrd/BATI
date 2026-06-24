package daemon

import (
	"errors"
	"fmt"
	"time"

	"bati/internal/model"
)

// TelemetryReader reads one live battery sample for a device.
type TelemetryReader interface {
	ReadTelemetry(deviceID string, screenOn bool) (model.Telemetry, error)
}

// TelemetryReaderFunc adapts a function to TelemetryReader.
type TelemetryReaderFunc func(deviceID string, screenOn bool) (model.Telemetry, error)

func (fn TelemetryReaderFunc) ReadTelemetry(deviceID string, screenOn bool) (model.Telemetry, error) {
	return fn(deviceID, screenOn)
}

// TelemetryBatcher is the persistence surface Recorder needs.
type TelemetryBatcher interface {
	AddTelemetry(model.Telemetry) error
	Flush() error
}

// Recorder reads live telemetry and persists it either buffered or immediately.
type Recorder struct {
	DeviceID    string
	Reader      TelemetryReader
	Batcher     TelemetryBatcher
	ScreenState func() bool
}

// RecordBuffered saves a telemetry sample through the batcher without forcing a flush.
func (r Recorder) RecordBuffered(reason string) error {
	return r.record(reason, false)
}

// RecordAndFlush saves a telemetry sample and immediately flushes it to durable storage.
func (r Recorder) RecordAndFlush(reason string) error {
	return r.record(reason, true)
}

func (r Recorder) record(reason string, flush bool) error {
	if r.DeviceID == "" {
		return errors.New("record telemetry: missing device id")
	}
	if r.Reader == nil {
		return errors.New("record telemetry: missing reader")
	}
	if r.Batcher == nil {
		return errors.New("record telemetry: missing batcher")
	}

	screenOn := true
	if r.ScreenState != nil {
		screenOn = r.ScreenState()
	}

	point, err := r.Reader.ReadTelemetry(r.DeviceID, screenOn)
	if err != nil {
		return fmt.Errorf("record telemetry %s: read: %w", reasonLabel(reason), err)
	}
	point.Timestamp = point.Timestamp.UTC()
	if point.Timestamp.IsZero() {
		point.Timestamp = time.Now().UTC()
	}

	if err := r.Batcher.AddTelemetry(point); err != nil {
		return fmt.Errorf("record telemetry %s: buffer: %w", reasonLabel(reason), err)
	}
	if flush {
		if err := r.Batcher.Flush(); err != nil {
			return fmt.Errorf("record telemetry %s: flush: %w", reasonLabel(reason), err)
		}
	}
	return nil
}

func reasonLabel(reason string) string {
	if reason == "" {
		return "sample"
	}
	return reason
}
