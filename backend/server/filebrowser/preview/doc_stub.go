//go:build !mupdf

package preview

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/indexing/iteminfo"
)

func (s *Service) GenerateImageFromDoc(ctx context.Context, file iteminfo.ExtendedFileInfo, tempFilePath string, pageNumber int) ([]byte, error) {
	if err := s.acquireDoc(ctx); err != nil {
		return nil, err
	}
	defer s.releaseDoc()

	s.docGenMutex.Lock()
	defer s.docGenMutex.Unlock()

	return nil, nil
}
