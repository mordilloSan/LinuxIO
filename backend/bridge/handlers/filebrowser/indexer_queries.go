package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

type indexerSubfolder = indexerapi.SubfolderResult

type indexerSearchResult = indexerapi.EntryResult

// fetchDirSizeFromIndexer queries the indexer daemon over its Unix socket for a cached directory size.
func fetchDirSizeFromIndexer(ctx context.Context, path string) (indexerapi.DirSizeResponse, error) {
	payload, err := indexer.DirSize(ctx, path)
	if err != nil {
		return indexerapi.DirSizeResponse{}, indexerError("dirsize", err)
	}
	return payload, nil
}

// fetchSubfoldersFromIndexer queries the indexer daemon for direct child folders with sizes
func fetchSubfoldersFromIndexer(ctx context.Context, path string) ([]apischema.SubfolderData, error) {
	folders, err := indexer.Subfolders(ctx, path)
	if err != nil {
		return nil, indexerError("subfolders", err)
	}
	return subfoldersFromIndexer(folders), nil
}

// searchInIndexer queries the indexer for files matching the search term
func searchInIndexer(ctx context.Context, query, limit, basePath string) ([]indexerSearchResult, error) {
	results, err := indexer.Search(ctx, query, limit, basePath)
	if err != nil {
		return nil, indexerError("search", err)
	}
	if results == nil {
		return []indexerSearchResult{}, nil
	}
	return results, nil
}

// dirSize returns the indexer's cached recursive size of a directory.
func dirSize(ctx context.Context, req apischema.PathRequest) (apischema.DirectorySizeData, error) {
	if req.Path == "" {
		return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:missing path")
	}

	root, err := fsroot.Open()
	if err != nil {
		return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	// Check if path exists and is a directory
	stat, err := root.Root.Stat(fsroot.ToRel(req.Path))
	if err != nil {
		slog.Debug("error stating directory", "path", req.Path, "error", err)
		return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:directory not found")
	}

	if !stat.IsDir() {
		return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:path is not a directory")
	}

	details, err := fetchDirSizeFromIndexer(ctx, req.Path)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching directory details from indexer", "path", req.Path, "error", err)
		return apischema.DirectorySizeData{}, fmt.Errorf("error fetching directory details: %w", err)
	}

	return apischema.DirectorySizeData{
		Size:        details.Size,
		FileCount:   details.Files,
		FolderCount: details.Dirs,
	}, nil
}

// subfolders gets direct child folders with their pre-calculated sizes.
func subfolders(ctx context.Context, req apischema.PathRequest) (apischema.SubfoldersResponse, error) {
	path := "/"
	if req.Path != "" {
		path = req.Path
	}

	root, err := fsroot.Open()
	if err != nil {
		return apischema.SubfoldersResponse{}, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	// Validate path exists and is a directory if not root.
	if path != "/" {
		stat, statErr := root.Root.Stat(fsroot.ToRel(path))
		if statErr != nil {
			slog.Debug("error stating directory", "path", path, "error", statErr)
			return apischema.SubfoldersResponse{}, fmt.Errorf("bad_request:directory not found")
		}
		if !stat.IsDir() {
			return apischema.SubfoldersResponse{}, fmt.Errorf("bad_request:path is not a directory")
		}
	}

	// Fetch subfolders from indexer (it will handle path validation)
	folders, err := fetchSubfoldersFromIndexer(ctx, path)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return apischema.SubfoldersResponse{}, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching subfolders from indexer", "path", path, "error", err)
		return apischema.SubfoldersResponse{}, fmt.Errorf("error fetching subfolders: %w", err)
	}

	return apischema.SubfoldersResponse{Subfolders: folders}, nil
}

func subfoldersFromIndexer(folders []indexerSubfolder) []apischema.SubfolderData {
	result := make([]apischema.SubfolderData, 0, len(folders))
	for _, folder := range folders {
		result = append(result, apischema.SubfolderData{
			Path: folder.Path, Size: folder.Size,
		})
	}
	return result
}

// searchFiles searches for files/directories in the indexer database.
func searchFiles(ctx context.Context, req apischema.FileSearchRequest) (apischema.SearchResponse, error) {
	if !indexerapi.SearchQueryAllowed(req.Query) {
		return apischema.SearchResponse{}, fmt.Errorf("bad_request:search query must contain at least %d characters", indexerapi.MinSearchQueryRunes)
	}

	limit := "100" // default limit
	if req.Limit != nil && *req.Limit != "" {
		limit = *req.Limit
	}

	basePath := "/" // default to root
	if req.BasePath != nil && *req.BasePath != "" {
		basePath = indexerapi.NormalizePath(*req.BasePath)
	}

	results, err := searchInIndexer(ctx, req.Query, limit, basePath)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return apischema.SearchResponse{}, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error searching indexer", "query", req.Query, "base_path", basePath, "error", err)
		return apischema.SearchResponse{}, fmt.Errorf("error searching files: %w", err)
	}

	return searchResponseFromIndexer(req.Query, results), nil
}

func searchResponseFromIndexer(_ string, results []indexerSearchResult) apischema.SearchResponse {
	response := apischema.SearchResponse{Results: make([]apischema.SearchResult, 0, len(results))}
	for _, result := range results {
		response.Results = append(response.Results, searchResultFromIndexer(result))
	}
	return response
}

func searchResultFromIndexer(result indexerSearchResult) apischema.SearchResult {
	isDir := result.Type == "folder"
	isRegularFile := result.Type == "file"
	var canOpenAsText *bool
	if isRegularFile {
		canOpen := result.Size < services.MaxTextFileBytes
		canOpenAsText = &canOpen
	}
	return apischema.SearchResult{
		IsDir: isDir, IsRegularFile: isRegularFile,
		ModTime: formatResourceModTime(result.ModTime), Name: result.Name, Path: result.Path, Size: result.Size,
		CanOpenAsText: canOpenAsText,
	}
}
