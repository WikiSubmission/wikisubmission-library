package db

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// --- CACHE CONFIGURATION ---

// CacheDuration determines how long the "All Files" view is stored in memory.
const CacheDuration = 10 * time.Minute

// explorerCache protects the cached data for the root view (empty query).
var explorerCache = struct {
	sync.RWMutex
	data      *ExplorerData
	timestamp time.Time
}{}

// --- TYPES ---

type ExplorerData struct {
	Query       string       `json:"query"`
	TotalFiles  int          `json:"total_files"` // Added for stats
	TotalDirs   int          `json:"total_dirs"`  // Added for stats
	HasMore     bool         `json:"has_more"`
	Directories DirectoryMap `json:"directories"`
}

type DirectoryMap struct {
	Files       []FileEntry               `json:"files"`       // Root level files
	Directories map[string]*DirectoryInfo `json:"directories"` // Nested folders
}

type DirectoryInfo struct {
	Name  string      `json:"name"`
	Path  string      `json:"path"`
	Files []FileEntry `json:"files"`
}

type FileEntry struct {
	Name string `json:"name"`
	Key  string `json:"key"`
	Size int64  `json:"size"`
}


// GetExplorerData is the main entry point.
// If query is empty, it attempts to serve from cache.
// If query is set, it performs a fresh DB search.
func (db *DB) GetExplorerData(ctx context.Context,query string, limit int) (ExplorerData, error) {
	// 1. Handle Empty Query (Cache Layer)
	if query == "" {
		explorerCache.RLock()
		if explorerCache.data != nil && time.Since(explorerCache.timestamp) < CacheDuration {
			slog.Info("Serving explorer data from cache", slog.String("query", "empty"))
			defer explorerCache.RUnlock()
			return *explorerCache.data, nil
		}
		explorerCache.RUnlock()

		// Cache miss or expired: Fetch everything
		slog.Info("Cache expired or empty, fetching all files from DB")
		
		// Assuming you have a method GetFiles() that returns []S3Object
		// If you only have SearchFiles, you might need a GetAllFiles() method
		objects, err := db.GetAllObjects(ctx) // Passing empty usually returns all in SQL logic
		if err != nil {
			return ExplorerData{}, err
		}
		hasMore := false
		if len(objects) > limit {
			hasMore = true
			objects = objects[:limit] 
		}

		// Build the tree
		data := BuildExplorerData(query, objects)
		data.HasMore = hasMore
		
		// Save to cache
		explorerCache.Lock()
		explorerCache.data = &data
		explorerCache.timestamp = time.Now()
		explorerCache.Unlock()


		return data, nil
	}

	// 2. Handle Active Search (Bypass Cache for specific results)
	slog.Info("Performing search query", slog.String("query", query))
	objects, err := db.SearchObjects(ctx, query, limit + 1)
	if err != nil {
		return ExplorerData{}, err
	}

	hasMore := false
    if len(objects) > limit {
        hasMore = true
        objects = objects[:limit] // Remove the extra item used for checking
    }
	
	data := BuildExplorerData(query, objects)
    data.HasMore = hasMore
    return data, nil
}

// BuildExplorerData transforms a flat list of S3Objects into a nested DirectoryMap.
//
// It iterates through the objects, parsing their FileKeys (e.g., "folder/subfolder/file.txt")
// to group them into logical directories. It also calculates basic statistics.
func BuildExplorerData(query string, objects []S3Object) ExplorerData {
	start := time.Now()
	
	data := ExplorerData{
		Query: query,
		TotalFiles: len(objects),
		Directories: DirectoryMap{
			Files:       []FileEntry{},
			Directories: make(map[string]*DirectoryInfo),
		},
	}

	slog.Debug("Starting to build explorer tree", 
		slog.String("query", query), 
		slog.Int("object_count", len(objects)),
	)

	for _, obj := range objects {
		// Split "folder/file.png" into ["folder", "file.png"]
		parts := strings.Split(obj.FileKey, "/")
		fileName := parts[len(parts)-1]

		// Case 1: Root File (no slashes in key, or just filename)
		if len(parts) == 1 {
			data.Directories.Files = append(data.Directories.Files, FileEntry{
				Name: fileName, 
				Key: obj.FileKey,
			})
			continue
		}

		// Case 2: Nested File
		// Logic: We group by the *immediate parent* folder to keep the UI simple (1 level deep visualization),
		// or use the full path as the map key to ensure uniqueness.
		
		// parts[:len(parts)-1] joins "a/b/c/file.txt" -> "a/b/c"
		dirPath := strings.Join(parts[:len(parts)-1], "/")
		
		// The display name is just "c"
		dirName := parts[len(parts)-2] 

		// Initialize map entry if missing
		if _, exists := data.Directories.Directories[dirPath]; !exists {
			data.Directories.Directories[dirPath] = &DirectoryInfo{
				Name:  dirName, // Display name (e.g., "Invoices")
				Path:  dirPath, // ID/Key (e.g., "finance/2023/Invoices")
				Files: []FileEntry{},
			}
		}

		// Add file to directory
		data.Directories.Directories[dirPath].Files = append(data.Directories.Directories[dirPath].Files, FileEntry{
			Name: fileName,
			Key:  obj.FileKey,
		})
	}

	data.TotalDirs = len(data.Directories.Directories)

	slog.Info("Finished building explorer tree",
		slog.String("duration", time.Since(start).String()),
		slog.Int("total_dirs", data.TotalDirs),
		slog.Int("total_files", data.TotalFiles),
	)

	return data
}