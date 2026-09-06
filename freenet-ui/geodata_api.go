package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxGeoDataSelectedFiles       = 16
	maxGeoDataQueryLength         = 253
	maxGeoDataCategoriesPerFile   = 128
	geoDataGenericFileError       = "geodata file is unreadable or invalid"
)

type geoDataFilesResponse struct {
	Success bool          `json:"success"`
	Files   []GeoDataFile `json:"files"`
	Error   string        `json:"error,omitempty"`
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
}

func (a *app) geoDataAssetDir() string {
	dir := strings.TrimSpace(a.cfg.GeoDataDir)
	if dir == "" {
		return defaultGeoDataAssetDir
	}
	return dir
}

func (a *app) handleGeoDataFiles(w http.ResponseWriter, _ *http.Request) {
	files, err := DiscoverGeoDataFiles(a.geoDataAssetDir())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, geoDataFilesResponse{Success: false, Files: []GeoDataFile{}, Error: "geodata directory is unavailable"})
		return
	}
	for i := range files {
		if files[i].Error != "" {
			files[i].Error = geoDataGenericFileError
		}
	}
	writeJSON(w, http.StatusOK, geoDataFilesResponse{Success: true, Files: files})
}

func (a *app) handleGeoDataSearch(w http.ResponseWriter, r *http.Request) {
	kind, err := parseGeoDataSearchKind(r.URL.Query().Get("kind"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, geoDataSearchResponse{Success: false, Kind: GeoDataUnknown, Matches: []geoDataSearchMatch{}, Error: err.Error()})
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > maxGeoDataQueryLength {
		writeJSON(w, http.StatusBadRequest, geoDataSearchResponse{Success: false, Kind: kind, Query: query, Matches: []geoDataSearchMatch{}, Error: "invalid geodata query"})
		return
	}

	requested := append([]string(nil), r.URL.Query()["file"]...)
	if len(requested) > maxGeoDataSelectedFiles {
		writeJSON(w, http.StatusBadRequest, geoDataSearchResponse{Success: false, Kind: kind, Query: query, Matches: []geoDataSearchMatch{}, Error: fmt.Sprintf("too many geodata files selected; max %d", maxGeoDataSelectedFiles)})
		return
	}
	for _, name := range requested {
		if !validGeoDataFileSelector(name) {
			writeJSON(w, http.StatusBadRequest, geoDataSearchResponse{Success: false, Kind: kind, Query: query, Matches: []geoDataSearchMatch{}, Error: "invalid geodata file selector"})
			return
		}
	}

	installed, err := DiscoverGeoDataFiles(a.geoDataAssetDir())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, geoDataSearchResponse{Success: false, Kind: kind, Query: query, Matches: []geoDataSearchMatch{}, Error: "geodata directory is unavailable"})
		return
	}
	byName := make(map[string]GeoDataFile, len(installed))
	for _, file := range installed {
		byName[file.Name] = file
	}

	selected, err := selectGeoDataFiles(kind, requested, installed, byName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, geoDataSearchResponse{Success: false, Kind: kind, Query: query, Matches: []geoDataSearchMatch{}, Error: err.Error()})
		return
	}

	matches := make([]geoDataSearchMatch, 0, len(selected))
	warnings := make([]string, 0)
	for _, file := range selected {
		if file.Error != "" || file.Kind == GeoDataUnknown {
			warnings = append(warnings, file.Name+": "+geoDataGenericFileError)
			continue
		}
		if file.Kind != kind {
			warnings = append(warnings, file.Name+": geodata type does not match requested kind")
			continue
		}
		data, err := readBoundedGeoDataFile(filepath.Join(a.geoDataAssetDir(), file.Name))
		if err != nil {
			warnings = append(warnings, file.Name+": "+geoDataGenericFileError)
			continue
		}

		var categories []string
		switch kind {
		case GeoDataSite:
			categories, err = SearchGeoSiteData(data, query)
		case GeoDataIP:
			categories, err = SearchGeoIPData(data, query)
		}
		if err != nil {
			warnings = append(warnings, file.Name+": search failed for this geodata file")
			continue
		}
		if len(categories) == 0 {
			continue
		}
		truncated := len(categories) > maxGeoDataCategoriesPerFile
		if truncated {
			categories = append([]string(nil), categories[:maxGeoDataCategoriesPerFile]...)
		}
		matches = append(matches, geoDataSearchMatch{File: file.Name, Kind: kind, Categories: categories, Truncated: truncated})
	}

	sort.Slice(matches, func(i, j int) bool { return matches[i].File < matches[j].File })
	sort.Strings(warnings)
	writeJSON(w, http.StatusOK, geoDataSearchResponse{Success: true, Kind: kind, Query: query, Matches: matches, Warnings: warnings})
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
