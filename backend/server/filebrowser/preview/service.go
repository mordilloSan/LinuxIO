package preview

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrUnsupportedFormat = errors.New("preview is not available for provided file format")
)

// Service coordinates preview-related helpers (image resizing, doc rendering).
// It currently only keeps the synchronization primitives required by the
// document rendering pipeline so the rest of the package can operate unchanged.
type Service struct {
	docGenMutex  sync.Mutex
	docSemaphore chan struct{}
}

// NewPreviewGenerator creates a preview Service constrained by the provided
// concurrency limit. The cache directory parameter is ignored but kept for API
// compatibility with the previous implementation.
func NewPreviewGenerator(concurrencyLimit int, _ string) *Service {
	if concurrencyLimit < 1 {
		concurrencyLimit = 1
	}
	return &Service{
		docSemaphore: make(chan struct{}, concurrencyLimit),
	}
}

func (s *Service) acquireDoc(ctx context.Context) error {
	select {
	case s.docSemaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releaseDoc() {
	select {
	case <-s.docSemaphore:
	default:
	}
}

func CacheKey(md5, previewSize string, percentage int) string {
	return fmt.Sprintf("%x%x%x", md5, previewSize, percentage)
}
