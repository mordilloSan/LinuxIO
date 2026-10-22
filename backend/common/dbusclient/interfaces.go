package dbusclient

const (
	DBusBusName = "org.freedesktop.DBus"
	DBusPath    = "/org/freedesktop/DBus"
	DBusIface   = "org.freedesktop.DBus"

	DBusListNames            = DBusIface + ".ListNames"
	DBusListActivatableNames = DBusIface + ".ListActivatableNames"
	DBusStartServiceByName   = DBusIface + ".StartServiceByName"

	PropertiesIface    = "org.freedesktop.DBus.Properties"
	PropertiesGet      = PropertiesIface + ".Get"
	ObjectManagerIface = "org.freedesktop.DBus.ObjectManager"

	SystemdBusName      = "org.freedesktop.systemd1"
	SystemdPath         = "/org/freedesktop/systemd1"
	SystemdManagerIface = "org.freedesktop.systemd1.Manager"
	SystemdUnitIface    = "org.freedesktop.systemd1.Unit"
	SystemdServiceIface = "org.freedesktop.systemd1.Service"
	SystemdTimerIface   = "org.freedesktop.systemd1.Timer"
	SystemdSocketIface  = "org.freedesktop.systemd1.Socket"

	LoginBusName  = "org.freedesktop.login1"
	LoginPath     = "/org/freedesktop/login1"
	LoginMgrIface = "org.freedesktop.login1.Manager"

	HostnameBusName = "org.freedesktop.hostname1"
	HostnamePath    = "/org/freedesktop/hostname1"
	HostnameIface   = "org.freedesktop.hostname1"

	TimedateBusName = "org.freedesktop.timedate1"
	TimedatePath    = "/org/freedesktop/timedate1"
	TimedateIface   = "org.freedesktop.timedate1"

	PackageKitBusName           = "org.freedesktop.PackageKit"
	PackageKitPath              = "/org/freedesktop/PackageKit"
	PackageKitIface             = "org.freedesktop.PackageKit"
	PackageKitCreateTransaction = PackageKitIface + ".CreateTransaction"
	PackageKitTransactionIface  = "org.freedesktop.PackageKit.Transaction"
	PackageKitOfflineIface      = "org.freedesktop.PackageKit.Offline"

	TunedBusName      = "com.redhat.tuned"
	TunedPath         = "/Tuned"
	TunedControlIface = "com.redhat.tuned.control"

	PowerProfilesBusName = "org.freedesktop.UPower.PowerProfiles"
	PowerProfilesPath    = "/org/freedesktop/UPower/PowerProfiles"
	PowerProfilesIface   = "org.freedesktop.UPower.PowerProfiles"
)
