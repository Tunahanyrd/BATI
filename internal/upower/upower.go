package upower

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	upowerDest       = "org.freedesktop.UPower"
	upowerPath       = "/org/freedesktop/UPower"
	upowerInterface  = "org.freedesktop.UPower"
	propertiesMember = "PropertiesChanged"
)

// GetBatteryDevicePaths calls EnumerateDevices on UPower to list all power supply path strings.
func GetBatteryDevicePaths(conn *dbus.Conn) ([]string, error) {
	obj := conn.Object(upowerDest, dbus.ObjectPath(upowerPath))
	var paths []dbus.ObjectPath

	err := obj.Call(upowerInterface+".EnumerateDevices", 0).Store(&paths)
	if err != nil {
		return nil, fmt.Errorf("call EnumerateDevices: %w", err)
	}

	result := make([]string, len(paths))
	for i, p := range paths {
		result[i] = string(p)
	}
	return result, nil
}

// SubscribePropertiesChanged configures subscription to D-Bus properties changes on UPower devices.
func SubscribePropertiesChanged(conn *dbus.Conn) (chan *dbus.Signal, error) {
	// Add D-Bus match rule to filter PropertiesChanged signals from UPower
	matchRule := fmt.Sprintf("type='signal',sender='%s',member='%s'", upowerDest, propertiesMember)
	call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, matchRule)
	if call.Err != nil {
		return nil, fmt.Errorf("add match rule: %w", call.Err)
	}

	signalChan := make(chan *dbus.Signal, 10)
	conn.Signal(signalChan)

	return signalChan, nil
}
