package preview

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/filebrowser/backend/adapters/fs/diskcache"
	"github.com/mordilloSan/filebrowser/backend/adapters/fs/fileutils"
	"github.com/mordilloSan/filebrowser/backend/common/settings"
	"github.com/mordilloSan/filebrowser/backend/indexing/iteminfo"
	"github.com/mordilloSan/go_logger/logger"
)

var (
	ErrUnsupportedFormat = errors.New("preview is not available for provided file format")
	ErrUnsupportedMedia  = errors.New("unsupported media type")
	service              *Service
)

type Service struct {
	fileCache    diskcache.Interface
	debug        bool
	docGenMutex  sync.Mutex    // Mutex to serialize access to doc generation
	docSemaphore chan struct{} // Semaphore for document generation
}

func NewPreviewGenerator(concurrencyLimit int, cacheDir string) *Service {
	limit := max(concurrencyLimit, 1)
	var fileCache diskcache.Interface
	// Use file cache if cacheDir is specified
	if cacheDir != "" {
		var err error
		fileCache, err = diskcache.NewFileCache(cacheDir)
		if err != nil {
			if cacheDir == "tmp" {
				logger.Errorf("The cache dir could not be created. Make sure the user that you executed the program with has access to create directories in the local path. filebrowser is trying to use the default `server.cacheDir: tmp` , but you can change this location if you need to. Please see configuration wiki for more information about this error. https://github.com/mordilloSan/filebrowser/wiki/Configuration")
			}
			logger.Fatalf("failed to create file cache path, which is now require to run the server: %v", err)
		}
	} else {
		// No-op cache if no cacheDir is specified
		fileCache = diskcache.NewNoOp()
	}
	// Create directories recursively
	err := os.MkdirAll(filepath.Join(settings.Config.Server.CacheDir, "thumbnails", "docs"), fileutils.PermDir)
	if err != nil {
		logger.Errorf("Error %v", err)
	}
	settings.Config.Server.MuPdfAvailable = docEnabled()
	logger.Debugf("MuPDF Enabled            : %v", settings.Config.Server.MuPdfAvailable)

	return &Service{
		fileCache: fileCache,
		debug:     false,
		// CGo library (go-fitz) is NOT thread-safe - restrict concurrency to the configured limit
		docSemaphore: make(chan struct{}, limit),
	}
}

// Document semaphore methods
func (s *Service) acquireDoc(ctx context.Context) error {
	select {
	case s.docSemaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releaseDoc() {
	<-s.docSemaphore
}

func StartPreviewGenerator(concurrencyLimit int, cacheDir string) error {
	if service != nil {
		logger.Errorf("WARNING: StartPreviewGenerator called multiple times! This will create multiple semaphores!")
	}
	service = NewPreviewGenerator(concurrencyLimit, cacheDir)
	return nil
}

func GetPreviewForFile(ctx context.Context, file iteminfo.ExtendedFileInfo, previewSize, url string, seekPercentage int) ([]byte, error) {
	if !file.HasPreview {
		return nil, ErrUnsupportedMedia
	}

	// Check if context is cancelled before starting
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Generate fast cache key based on file metadata
	hasher := md5.New()
	cacheString := fmt.Sprintf("%s:%d:%s", file.RealPath, file.Size, file.ModTime.Format(time.RFC3339Nano))
	_, _ = hasher.Write([]byte(cacheString))
	cacheHash := hex.EncodeToString(hasher.Sum(nil))

	cacheKey := CacheKey(cacheHash, previewSize, seekPercentage)
	if data, found, err := service.fileCache.Load(ctx, cacheKey); err != nil {
		return nil, fmt.Errorf("failed to load from cache: %w", err)
	} else if found {
		return data, nil
	}
	return GeneratePreviewWithMD5(ctx, file, previewSize, seekPercentage, cacheHash)
}

func GeneratePreviewWithMD5(ctx context.Context, file iteminfo.ExtendedFileInfo, previewSize string, seekPercentage int, fileMD5 string) ([]byte, error) {
	// Note: fileMD5 is actually a cache hash (metadata-based), not a true file content MD5
	// Validate that cache hash is not empty to prevent cache corruption
	if fileMD5 == "" {
		errorMsg := fmt.Sprintf("Cache hash is empty for file: %s (path: %s)", file.Name, file.RealPath)
		logger.Errorf("Preview generation failed: %s", errorMsg)
		return nil, fmt.Errorf("preview generation failed: %s", errorMsg)
	}

	// Check if context is cancelled before starting
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	ext := strings.ToLower(filepath.Ext(file.Name))
	var (
		err        error
		imageBytes []byte
	)

	// Generate thumbnail image
	hasher := md5.New()
	_, _ = hasher.Write([]byte(CacheKey(fileMD5, previewSize, seekPercentage)))
	hash := hex.EncodeToString(hasher.Sum(nil))
	// Generate an image from office document
	if iteminfo.HasDocConvertableExtension(file.Name, file.Type) {
		tempFilePath := filepath.Join(settings.Config.Server.CacheDir, "thumbnails", "docs", hash) + ".txt"
		imageBytes, err = service.GenerateImageFromDoc(ctx, file, tempFilePath, 0) // 0 for the first page
		if err != nil {
			return nil, fmt.Errorf("failed to create image for PDF file: %w", err)
		}
	} else if strings.HasPrefix(file.Type, "image") {
		imageBytes, err = os.ReadFile(file.RealPath)
		if err != nil {
			logger.Errorf("Failed to read image file '%s' (path: %s): %v", file.Name, file.RealPath, err)
			return nil, fmt.Errorf("failed to read image file: %w", err)
		}
	} else {
		return nil, fmt.Errorf("unsupported media type: %s", ext)
	}

	// Check if context was cancelled during processing
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if len(imageBytes) < 100 {
		logger.Errorf("Generated image too small for '%s' (type: %s): %d bytes - likely an error occurred",
			file.Name, file.Type, len(imageBytes))
		return nil, fmt.Errorf("generated image is too small, likely an error occurred: %d bytes", len(imageBytes))
	}

	if previewSize != "original" {
		// resize image
		resizedBytes, err := service.CreatePreview(imageBytes, previewSize)
		if err != nil {
			return nil, fmt.Errorf("failed to resize preview image: %w", err)
		}
		// Cache and return
		cacheKey := CacheKey(fileMD5, previewSize, seekPercentage)
		if err := service.fileCache.Store(ctx, cacheKey, resizedBytes); err != nil {
			logger.Errorf("failed to cache resized image: %v", err)
		}
		return resizedBytes, nil
	} else {
		cacheKey := CacheKey(fileMD5, previewSize, seekPercentage)
		if err := service.fileCache.Store(ctx, cacheKey, imageBytes); err != nil {
			logger.Errorf("failed to cache original image: %v", err)
		}
		return imageBytes, nil
	}
}

func GeneratePreview(ctx context.Context, file iteminfo.ExtendedFileInfo, previewSize string, seekPercentage int) ([]byte, error) {
	// Generate fast metadata-based cache key
	hasher := md5.New()
	cacheString := fmt.Sprintf("%s:%d:%s", file.RealPath, file.Size, file.ModTime.Format(time.RFC3339Nano))
	_, _ = hasher.Write([]byte(cacheString))
	cacheHash := hex.EncodeToString(hasher.Sum(nil))

	return GeneratePreviewWithMD5(ctx, file, previewSize, seekPercentage, cacheHash)
}

func (s *Service) CreatePreview(data []byte, previewSize string) ([]byte, error) {
	var (
		width   int
		height  int
		options []Option
	)

	switch previewSize {
	case "small":
		width, height = 256, 256
		options = []Option{WithMode(ResizeModeFit), WithQuality(QualityMedium), WithFormat(FormatJpeg)}
	default:
		return nil, ErrUnsupportedFormat
	}

	input := bytes.NewReader(data)
	output := &bytes.Buffer{}

	if err := s.Resize(input, width, height, output, options...); err != nil {
		return nil, err
	}

	return output.Bytes(), nil
}

func CacheKey(md5, previewSize string, percentage int) string {
	key := fmt.Sprintf("%x%x%x", md5, previewSize, percentage)
	return key
}

func DelThumbs(ctx context.Context, file iteminfo.ExtendedFileInfo) {
	// Generate metadata-based cache hash for deletion
	hasher := md5.New()
	cacheString := fmt.Sprintf("%s:%d:%s", file.RealPath, file.Size, file.ModTime.Format(time.RFC3339Nano))
	_, _ = hasher.Write([]byte(cacheString))
	cacheHash := hex.EncodeToString(hasher.Sum(nil))

	if err := service.fileCache.Delete(ctx, CacheKey(cacheHash, "small", 0)); err != nil {
		logger.Debugf("Could not delete thumbnail: %v", file.Name)
	}
}

// CheckValidFFprobe checks for a valid ffprobe executable.
// If a path is provided, it looks there. Otherwise, it searches the system's PATH.
func CheckValidFFprobe(path string) (string, error) {
	return checkExecutable(path, "ffprobe")
}

// checkExecutable is an internal helper function to find and validate an executable.
// It checks a specific path if provided, otherwise falls back to searching the system PATH.
func checkExecutable(providedPath, execName string) (string, error) {
	var finalPath string
	var err error

	if providedPath != "" {
		// A path was provided, so we'll use it.
		finalPath = filepath.Join(providedPath, execName)
	} else {
		// No path was provided, so search the system's PATH for the executable.
		finalPath, err = exec.LookPath(execName)
		if err != nil {
			// The executable was not found in the system's PATH.
			return "", err
		}
	}

	// Verify the executable is valid by running the "-version" command.
	cmd := exec.Command(finalPath, "-version")
	err = cmd.Run()

	return finalPath, err
}
