package daemon

import (
	"errors"
	"testing"
	"time"

	"bati/internal/model"
)

type fakeBatcher struct {
	buffered  []model.Telemetry
	persisted []model.Telemetry
	flushes   int
}

func (batcher *fakeBatcher) AddTelemetry(point model.Telemetry) error {
	batcher.buffered = append(batcher.buffered, point)
	return nil
}

func (batcher *fakeBatcher) Flush() error {
	batcher.flushes++
	batcher.persisted = append(batcher.persisted, batcher.buffered...)
	batcher.buffered = nil
	return nil
}

func TestRecordAndFlushPersistsImmediately(t *testing.T) {
	location := time.FixedZone("test", 3*60*60)
	readerSawScreenOn := false
	reader := TelemetryReaderFunc(func(deviceID string, screenOn bool) (model.Telemetry, error) {
		if deviceID != "BAT0" {
			t.Fatalf("unexpected device id: %s", deviceID)
		}
		readerSawScreenOn = screenOn
		return model.Telemetry{
			Timestamp: time.Date(2026, 6, 24, 12, 0, 0, 0, location),
			Capacity:  93,
			Status:    "Not charging",
		}, nil
	})
	batcher := &fakeBatcher{}
	recorder := Recorder{
		DeviceID:    "BAT0",
		Reader:      reader,
		Batcher:     batcher,
		ScreenState: func() bool { return true },
	}

	if err := recorder.RecordAndFlush("startup"); err != nil {
		t.Fatalf("RecordAndFlush failed: %v", err)
	}
	if !readerSawScreenOn {
		t.Fatal("reader did not receive current screen state")
	}
	if batcher.flushes != 1 || len(batcher.persisted) != 1 || len(batcher.buffered) != 0 {
		t.Fatalf("expected one immediately persisted sample, got flushes=%d persisted=%d buffered=%d",
			batcher.flushes, len(batcher.persisted), len(batcher.buffered))
	}
	if batcher.persisted[0].Timestamp.Location() != time.UTC {
		t.Fatalf("expected persisted timestamp to be UTC, got %s", batcher.persisted[0].Timestamp.Location())
	}
}

func TestRecordBufferedDoesNotFlush(t *testing.T) {
	batcher := &fakeBatcher{}
	recorder := Recorder{
		DeviceID: "BAT0",
		Reader: TelemetryReaderFunc(func(string, bool) (model.Telemetry, error) {
			return model.Telemetry{Timestamp: time.Now(), Capacity: 91}, nil
		}),
		Batcher: batcher,
	}

	if err := recorder.RecordBuffered("periodic"); err != nil {
		t.Fatalf("RecordBuffered failed: %v", err)
	}
	if batcher.flushes != 0 || len(batcher.persisted) != 0 || len(batcher.buffered) != 1 {
		t.Fatalf("expected buffered-only sample, got flushes=%d persisted=%d buffered=%d",
			batcher.flushes, len(batcher.persisted), len(batcher.buffered))
	}
}

func TestRecordReportsReadErrors(t *testing.T) {
	recorder := Recorder{
		DeviceID: "BAT0",
		Reader: TelemetryReaderFunc(func(string, bool) (model.Telemetry, error) {
			return model.Telemetry{}, errors.New("sysfs read failed")
		}),
		Batcher: &fakeBatcher{},
	}

	if err := recorder.RecordAndFlush("resume"); err == nil {
		t.Fatal("expected read error")
	}
}
