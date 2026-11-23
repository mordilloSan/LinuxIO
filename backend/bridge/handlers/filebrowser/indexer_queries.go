package filebrowser

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// fetchDirSizeFromIndexer queries the indexer daemon over its Unix socket for a cached directory size.
func fetchDirSizeFromIndexer(ctx context.Context, path string) (indexerapi.DirSizeResponse, error) {
	payload, err := indexer.DirSize(ctx, path)
	if err != nil {
		return indexerapi.DirSizeResponse{}, indexerError("dirsize", err)
	}
	return payload, nil
}

// fetchSubfoldersFromIndexer queries the indexer daemon for direct child
// folders with sizes, and whether an update that can change them still waits.
func fetchSubfoldersFromIndexer(ctx context.Context, path string) ([]apischema.SubfolderData, bool, error) {
	// Read before the indexer is asked: see indexerUpdatesInFlight.
	pending := indexerUpdatesInFlight()
	resp, err := indexer.Subfolders(ctx, path)
	if err != nil {
		return nil, false, indexerError("subfolders", err)
	}
	return subfoldersFromIndexer(resp.Subfolders), pending || resp.Pending, nil
}

// searchInIndexer queries the indexer for files matching the search term
func searchInIndexer(ctx context.Context, query, limit, basePath string) ([]indexerapi.EntryResult, error) {
	results, err := indexer.Search(ctx, query, limit, basePath)
	if err != nil {
		return nil, indexerError("search", err)
	}
	if results == nil {
		return []indexerapi.EntryResult{}, nil
	}
	return results, nil
}

// dirSize returns the indexer's cached recursive size of a directory.
func dirSize(ctx context.Context, req apischema.PathRequest) (apischema.DirectorySizeData, error) {
	if req.Path == "" {
		return apischema.DirectorySizeData{}, fmt.Errorf("%w: missing path", bridgeipc.ErrInvalidArgs)
	}

	root, err := fsroot.Open()
	if err != nil {
		return apischema.DirectorySizeData{}, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()

	// The indexer knows a folder by its real path.
	path, isDir, err := iteminfo.ResolveSymlinksAt(ctx, root, req.Path)
	if err != nil {
		return apischema.DirectorySizeData{}, fsError("stat directory", err)
	}
	if !isDir {
		return apischema.DirectorySizeData{}, fmt.Errorf("%w: path is not a directory", bridgeipc.ErrInvalidArgs)
	}

	// Read before the indexer is asked: see indexerUpdatesInFlight.
	pending := indexerUpdatesInFlight()
	details, err := fetchDirSizeFromIndexer(ctx, path)
	if err != nil {
		return apischema.DirectorySizeData{}, fmt.Errorf("error fetching directory details: %w", err)
	}

	return apischema.DirectorySizeData{
		Size:        details.Size,
		FileCount:   details.Files,
		FolderCount: details.Dirs,
		Pending:     pending || details.Pending,
	}, nil
}

// subfolders gets direct child folders with their pre-calculated sizes.
func subfolders(ctx context.Context, req apischema.PathRequest) (apischema.SubfoldersResponse, error) {
	requested := fsroot.CleanAbsPath(req.Path)

	root, err := fsroot.Open()
	if err != nil {
		return apischema.SubfoldersResponse{}, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()

	// The indexer knows a folder by its real path.
	path, isDir, err := iteminfo.ResolveSymlinksAt(ctx, root, requested)
	if err != nil {
		return apischema.SubfoldersResponse{}, fsError("stat directory", err)
	}
	if !isDir {
		return apischema.SubfoldersResponse{}, fmt.Errorf("%w: path is not a directory", bridgeipc.ErrInvalidArgs)
	}

	// Fetch subfolders from indexer (it will handle path validation)
	folders, pending, err := fetchSubfoldersFromIndexer(ctx, path)
	if err != nil {
		return apischema.SubfoldersResponse{}, fmt.Errorf("error fetching subfolders: %w", err)
	}
	for i := range folders {
		folders[i].Path = pathAsRequested(requested, path, folders[i].Path)
	}

	return apischema.SubfoldersResponse{Subfolders: folders, Pending: pending}, nil
}

// pathAsRequested names p, a path below resolved, below requested instead,
// the path the page asked by, which resolves to resolved: the page matches
// what comes back to the entries it listed there. A path outside resolved is
// returned as it is.
func pathAsRequested(requested, resolved, p string) string {
	rel, err := filepath.Rel(resolved, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return p
	}
	return filepath.Join(requested, rel)
}

func subfoldersFromIndexer(folders []indexerapi.SubfolderResult) []apischema.SubfolderData {
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
		return apischema.SearchResponse{}, fmt.Errorf("%w: search query must contain at least %d characters", bridgeipc.ErrInvalidArgs, indexerapi.MinSearchQueryRunes)
	}

	limit := "100" // default limit
	if req.Limit != nil && *req.Limit != "" {
		limit = *req.Limit
	}

	basePath := "/" // default to root
	if req.BasePath != nil && *req.BasePath != "" {
		basePath = indexerapi.NormalizePath(*req.BasePath)
	}

	// The indexer knows a folder by its real path; the results are named
	// below the folder the page searched in.
	resolved, err := indexedPath(ctx, basePath)
	if err != nil {
		return apischema.SearchResponse{}, err
	}
	results, err := searchInIndexer(ctx, req.Query, limit, resolved)
	if err != nil {
		return apischema.SearchResponse{}, fmt.Errorf("error searching files: %w", err)
	}
	for i := range results {
		results[i].Path = pathAsRequested(basePath, resolved, results[i].Path)
	}

	return searchResponseFromIndexer(results), nil
}

// indexedPath is the path the indexer knows the folder at path by: its real
// path, or path itself when it cannot be resolved, which the indexer then
// answers for.
func indexedPath(ctx context.Context, path string) (string, error) {
	root, err := fsroot.Open()
	if err != nil {
		return "", fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()
	resolved, _, err := iteminfo.ResolveSymlinksAt(ctx, root, path)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		slog.Debug("failed to resolve a path for the indexer", "path", path, "error", err)
		return path, nil
	}
	return resolved, nil
}

func searchResponseFromIndexer(results []indexerapi.EntryResult) apischema.SearchResponse {
	response := apischema.SearchResponse{Results: make([]apischema.SearchResult, 0, len(results))}
	for _, result := range results {
		response.Results = append(response.Results, searchResultFromIndexer(result))
	}
	return response
}

func searchResultFromIndexer(result indexerapi.EntryResult) apischema.SearchResult {
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
