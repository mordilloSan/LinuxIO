package hostname

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

var hostnameIface = dbusclient.Hostname.Interface(dbusclient.HostnameIface)

func SetHostname(ctx context.Context, hostname string) error {
	return withHostnameSession(ctx, func(session dbusclient.SystemSession) error {
		return session.Call(hostnameIface.Method("SetStaticHostname"), dbusclient.CallPolicy{}, hostname, false)
	})
}

func withHostnameSession(ctx context.Context, fn func(dbusclient.SystemSession) error) error {
	return dbusclient.Hostname.UseSession(ctx, fn)
}
