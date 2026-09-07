package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxGeoDataSelectedFiles     = 16
	maxGeoDataQueryLength       = 253
	maxGeoDataCategoriesPerFile = 128
	geoDataGenericFileError     = "geodata file is unreadable or invalid"
	geoDataSearchDisabledError  = "GeoData search temporarily disabled for memory safety"
)

type geoDataFilesResponse struct {
	Success       bool          `json:"success"`
	Files         []GeoDataFile `json:"files"`
	SearchEnabled bool          `json:"search_enabled"`
	Error         string        `json:"error,omitempty"`
}

type geoDataSearchMatch struct {
	File       string      `json:"file"`
	Kind       GeoDataKind `json:"kind"`
	Categories []string    `json:"categories"`
	Truncated  bool        `json:"truncated,omitempty"`
}

type geoDataSearchResponse struct {
	Success  bool                 `json:"success"`
	Kind     GeoDataKind          `json:"kind"`
	Query    string               `json:"query"`
	Matches  []geoDataSearchMatch `json:"matches"`
	Warnings []string             `json:"warnings,omitempty"`
	Error    string               `json:"error,omitempty"`
}

func registerGeoDataAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/geodata/files", a.requireAuth(a.handleGeoDataFiles))
	mux.HandleFunc("GET /api/geodata/search", a.requireAuth(a.handleGeoDataSearch))
	registerPolicyPreviewAPI(mux, a)
	registerHardwareCapabilityAPI(mux, a)
	registerBestServerAPI(mux, a)
}

func (a *app) geoDataAssetDir() string {
	dir := strings.TrimSpace(a.cfg.GeoDataDir)
	if dir == "" {
		return defaultGeoDataAssetDir
	}
	return dir
}

// listGeoDataFileMetadata is intentionally metadata-only. The Network page calls
// /api/geodata/files automatically, so discovery must never read or decode large
// Xray .dat files on a memory-constrained router. Filename classification is only
// a UI hint while content search is disabled; it is not trusted for parsing.
func listGeoDataFileMetadata(assetDir string) ([]GeoDataFile, error) {
	entries, err := os.ReadDir(assetDir)
	if err != nil {
		return nil, err
	}
	files := make([]GeoDataFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(entry.Name()), ".dat") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		files = append(files, GeoDataFile{
			Name: entry.Name(),
			Kind: geoDataKindFilenameHint(entry.Name()),
			Size: info.Size(),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func geoDataKindFilenameHint(name string) GeoDataKind {
	lower := strings.ToLower(filepath.Base(name))
	switch {
	case strings.Contains(lower, "geosite"):
		return GeoDataSite
	case strings.Contains(lower, "geoip"):
		return GeoDataIP
	default:
		return GeoDataUnknown
	}
}

func (a *app) handleGeoDataFiles(w http.ResponseWriter, _ *http.Request) {
	files, err := listGeoDataFileMetadata(a.geoDataAssetDir())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, geoDataFilesResponse{Success: false, Files: []GeoDataFile{}, SearchEnabled: false, Error: "geodata directory is unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, geoDataFilesResponse{Success: true, Files: files, SearchEnabled: false})
}

func (a *app) handleGeoDataSearch(w http.ResponseWriter, _ *http.Request) {
	// P0 containment for 512 MiB routers: the legacy parser reads a complete
	// .dat file into memory. Do not touch the asset directory at all from this
	// endpoint until the search implementation is replaced by a bounded-memory
	// streaming parser with cancellation and total decode limits.
	writeJSON(w, http.StatusServiceUnavailable, geoDataSearchResponse{
		Success: false,
		Kind:    GeoDataUnknown,
		Matches: []geoDataSearchMatch{},
		Error:   geoDataSearchDisabledError,
	})
}

func parseGeoDataSearchKind(raw string) (GeoDataKind, error) {
	switch GeoDataKind(strings.ToLower(strings.TrimSpace(raw))) {
	case GeoDataSite:
		return GeoDataSite, nil
	case GeoDataIP:
		return GeoDataIP, nil
	default:
		return GeoDataUnknown, fmt.Errorf("kind must be geosite or geoip")
	}
}

func validateGeoDataSearchQuery(kind GeoDataKind, query string) error {
	switch kind {
	case GeoDataSite:
		_, err := SearchGeoSiteData(nil, query)
		return err
	case GeoDataIP:
		_, err := SearchGeoIPData(nil, query)
		return err
	default:
		return fmt.Errorf("unsupported geodata kind")
	}
}

func validGeoDataFileSelector(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return false
	}
	return strings.EqualFold(filepath.Ext(name), ".dat")
}

func selectGeoDataFiles(kind GeoDataKind, requested []string, installed []GeoDataFile, byName map[string]GeoDataFile) ([]GeoDataFile, error) {
	if len(requested) == 0 {
		selected := make([]GeoDataFile, 0, len(installed))
		for _, file := range installed {
			if file.Kind == kind || file.Kind == GeoDataUnknown {
				selected = append(selected, file)
			}
		}
		if len(selected) > maxGeoDataSelectedFiles {
			return nil, fmt.Errorf("too many installed geodata files; select up to %d explicitly", maxGeoDataSelectedFiles)
		}
		return selected, nil
	}

	seen := make(map[string]struct{}, len(requested))
	selected := make([]GeoDataFile, 0, len(requested))
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		file, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("selected geodata file is not installed")
		}
		if file.Kind != GeoDataUnknown && file.Kind != kind {
			return nil, fmt.Errorf("selected geodata file has incompatible type")
		}
		selected = append(selected, file)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })
	return selected, nil
}
