package dbusclient

import godbus "github.com/godbus/dbus/v5"

var (
	DBus = SystemObject{
		Subsystem: "dbus",
		BusName:   DBusBusName,
		Path:      godbus.ObjectPath(DBusPath),
	}

	SystemdManager = SystemObject{
		Subsystem: "systemd",
		BusName:   SystemdBusName,
		Path:      godbus.ObjectPath(SystemdPath),
	}

	Login1Manager = SystemObject{
		Subsystem: "login1",
		BusName:   LoginBusName,
		Path:      godbus.ObjectPath(LoginPath),
	}

	Hostname = SystemObject{
		Subsystem: "hostname",
		BusName:   HostnameBusName,
		Path:      godbus.ObjectPath(HostnamePath),
	}

	Timedate = SystemObject{
		Subsystem: "timedate",
		BusName:   TimedateBusName,
		Path:      godbus.ObjectPath(TimedatePath),
	}

	PackageKit = SystemObject{
		Subsystem:   "packagekit",
		BusName:     PackageKitBusName,
		Path:        godbus.ObjectPath(PackageKitPath),
		Unavailable: ErrPackageKitUnavailable,
	}

	Tuned = SystemObject{
		Subsystem: "tuned",
		BusName:   TunedBusName,
		Path:      godbus.ObjectPath(TunedPath),
	}

	PowerProfiles = SystemObject{
		Subsystem: "power-profiles",
		BusName:   PowerProfilesBusName,
		Path:      godbus.ObjectPath(PowerProfilesPath),
	}
)
