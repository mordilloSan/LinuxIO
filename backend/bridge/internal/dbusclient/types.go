package dbusclient

import godbus "github.com/godbus/dbus/v5"

// ObjectPath is a D-Bus object path exposed through dbusclient so handlers do
// not need to import the transport package for dynamic object references.
type ObjectPath = godbus.ObjectPath

// BusObject is a D-Bus object handle exposed through dbusclient for helpers
// that need to read properties from dynamic objects returned by D-Bus calls.
type BusObject = godbus.BusObject
