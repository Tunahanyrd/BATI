package logind

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	logindDest      = "org.freedesktop.login1"
	logindPath      = "/org/freedesktop/login1"
	logindInterface = "org.freedesktop.login1.Manager"
	sleepSignal     = "PrepareForSleep"
)

// SubscribeSleepSignals subscribes to systemd-logind sleep/resume D-Bus signals.
// It returns a channel that receives a signal when sleep status changes.
func SubscribeSleepSignals(conn *dbus.Conn) (chan *dbus.Signal, error) {
	matchRule := fmt.Sprintf("type='signal',sender='%s',path='%s',interface='%s',member='%s'",
		logindDest, logindPath, logindInterface, sleepSignal)

	call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule)
	if call.Err != nil {
		return nil, fmt.Errorf("add logind sleep match rule: %w", call.Err)
	}

	signalChan := make(chan *dbus.Signal, 5)
	conn.Signal(signalChan)

	return signalChan, nil
}

// DecodePrepareForSleep decodes the boolean payload from the PrepareForSleep signal.
// Returns true if entering sleep (suspend), false if exiting sleep (resume).
func DecodePrepareForSleep(sig *dbus.Signal) (bool, error) {
	if sig.Name != logindInterface+"."+sleepSignal {
		return false, fmt.Errorf("unexpected signal name: %s", sig.Name)
	}

	if len(sig.Body) == 0 {
		return false, fmt.Errorf("empty signal body")
	}

	isEnteringSleep, ok := sig.Body[0].(bool)
	if !ok {
		return false, fmt.Errorf("signal argument is not boolean: %v", sig.Body[0])
	}

	return isEnteringSleep, nil
}
