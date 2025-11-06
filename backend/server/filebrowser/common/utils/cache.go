package utils

import (
	"time"

	"github.com/mordilloSan/filebrowser/backend/common/cache"
)

var DiskUsageCache = cache.NewCache[bool](30*time.Second, 24*time.Hour)
