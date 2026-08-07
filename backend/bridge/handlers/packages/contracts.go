package packages

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func updatesToAPI(values []UpdateDetail) []apischema.Update {
	result := make([]apischema.Update, len(values))
	for i, value := range values {
		result[i] = updateToAPI(value)
	}
	return result
}

func updateToAPI(value UpdateDetail) apischema.Update {
	return apischema.Update{
		PackageID: value.PackageID, Summary: value.Summary, Version: value.Version, Issued: value.Issued,
		Changelog: value.Changelog, CVE: value.CVEs, Restart: int(value.Restart), State: int(value.State), InfoEnum: value.InfoEnum,
	}
}
