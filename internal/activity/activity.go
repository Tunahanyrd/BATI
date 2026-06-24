package activity

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	gnomeScreensaverDest      = "org.gnome.ScreenSaver"
	gnomeScreensaverInterface = "org.gnome.ScreenSaver"

	fdScreensaverDest      = "org.freedesktop.ScreenSaver"
	fdScreensaverInterface = "org.freedesktop.ScreenSaver"

	screensaverSignal = "ActiveChanged"
)

// SubscribeToActivitySignals subscribes to screensaver status signals on the user's Session Bus.
func SubscribeToActivitySignals(conn *dbus.Conn) (chan *dbus.Signal, error) {
	// GNOME Screensaver rule
	gnomeRule := fmt.Sprintf("type='signal',sender='%s',interface='%s',member='%s'",
		gnomeScreensaverDest, gnomeScreensaverInterface, screensaverSignal)
	if call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, gnomeRule); call.Err != nil {
		// Log but do not fail; desktop might be KDE or generic
	}

	// Freedesktop ScreenSaver rule (KDE / general)
	fdRule := fmt.Sprintf("type='signal',sender='%s',interface='%s',member='%s'",
		fdScreensaverDest, fdScreensaverInterface, screensaverSignal)
	if call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, fdRule); call.Err != nil {
		// Log but do not fail
	}

	signalChan := make(chan *dbus.Signal, 10)
	conn.Signal(signalChan)

	return signalChan, nil
}

// ParseActiveChanged checks if the signal is a screensaver ActiveChanged event.
// It returns (isScreensaverActive, isParsed).
func ParseActiveChanged(sig *dbus.Signal) (bool, bool) {
	if sig.Name != gnomeScreensaverInterface+"."+screensaverSignal &&
		sig.Name != fdScreensaverInterface+"."+screensaverSignal {
		return false, false
	}

	if len(sig.Body) == 0 {
		return false, false
	}

	active, ok := sig.Body[0].(bool)
	if !ok {
		return false, false
	}

	return active, true
}
