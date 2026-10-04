package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxGeoDataSuggestions   = 24
	maxGeoDataDNSAnswers    = 4
	maxGeoDataSuggestLength = 512
	geoDataDNSTimeout       = 1500 * time.Millisecond
)

var geoDataLookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

type geoDataSuggestion struct {
	File        string      `json:"file"`
	Kind        GeoDataKind `json:"kind"`
	Category    string      `json:"category"`
	Selector    string      `json:"selector"`
	ExtSelector string      `json:"ext_selector,omitempty"`
	Match       string      `json:"match"`
	Evidence    []string    `json:"evidence,omitempty"`
}

type geoDataSuggestResponse struct {
	Success     bool                `json:"success"`
	Kind        GeoDataKind         `json:"kind"`
	Query       string              `json:"query"`
	Mode        string              `json:"mode"`
	Suggestions []geoDataSuggestion `json:"suggestions"`
	Resolved    []string            `json:"resolved,omitempty"`
	Warnings    []string            `json:"warnings,omitempty"`
	Mutation    string              `json:"mutation"`
	Error       string              `json:"error,omitempty"`
}

type geoDataSuggestQuery struct {
	Mode  string
	Value string
}

func registerGeoDataSuggestAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/geodata/suggest", a.requireAuth(a.handleGeoDataSuggest))
}

func (a *app) handleGeoDataSuggest(w http.ResponseWriter, r *http.Request) {
	kind, err := parseGeoDataSearchKind(r.URL.Query().Get("kind"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, geoDataSuggestResponse{Success: false, Kind: GeoDataUnknown, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: err.Error()})
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("q"))
	if raw == "" || len(raw) > maxGeoDataSuggestLength {
		writeJSON(w, http.StatusBadRequest, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: "invalid geodata suggestion query"})
		return
	}
	query, err := classifyGeoDataSuggestQuery(kind, raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: err.Error()})
		return
	}

	installed, err := listGeoDataFileMetadata(a.geoDataAssetDir())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Mode: query.Mode, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: "geodata directory is unavailable"})
		return
	}
	byName := make(map[string]GeoDataFile, len(installed))
	for _, file := range installed {
		byName[file.Name] = file
	}
	selected, err := selectGeoDataFiles(kind, r.URL.Query()["file"], installed, byName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Mode: query.Mode, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: err.Error()})
		return
	}

	ctx, cancel := geoDataSearchContext(r.Context())
	defer cancel()

	resolved := []string{}
	if query.Mode == "dns" {
		resolved, err = resolveGeoDataHost(ctx, query.Value)
		if err != nil {
			writeJSON(w, http.StatusOK, geoDataSuggestResponse{
				Success: true, Kind: kind, Query: raw, Mode: query.Mode,
				Suggestions: []geoDataSuggestion{}, Resolved: []string{},
				Warnings: []string{"DNS lookup: " + err.Error()}, Mutation: "NONE",
			})
			return
		}
	}

	suggestions := make(map[string]*geoDataSuggestion)
	warnings := make([]string, 0)
	scanWarnings := make([]string, 0)
	var budgetUsed int64
	for _, file := range selected {
		if err := geoDataContextErr(ctx); err != nil {
			writeJSON(w, http.StatusRequestTimeout, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Mode: query.Mode, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: "geodata suggestion search timed out"})
			return
		}
		scans := 1
		if query.Mode == "dns" {
			scans = len(resolved)
		}
		if scans < 1 {
			scans = 1
		}
		needed := file.Size * int64(scans)
		if file.Size < 0 || file.Size > maxGeoDataFileSize {
			warnings = append(warnings, geoDataWarning(file.Name, "file exceeds safe search size"))
			continue
		}
		if needed < 0 || needed > maxGeoDataSearchTotalBytes-budgetUsed {
			warnings = append(warnings, geoDataWarning(file.Name, "request byte budget exceeded; file skipped"))
			continue
		}
		budgetUsed += needed

		path := filepath.Join(a.geoDataAssetDir(), file.Name)
		switch query.Mode {
		case "prefix":
			result, scanErr := searchGeoDataCategoryPrefixFileStream(ctx, path, kind, query.Value, maxGeoDataSuggestions)
			if scanErr != nil {
				if isGeoDataTimeout(scanErr) {
					writeJSON(w, http.StatusRequestTimeout, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Mode: query.Mode, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: "geodata suggestion search timed out"})
					return
				}
				scanWarnings = append(scanWarnings, geoDataWarning(file.Name, geoDataGenericFileError))
				continue
			}
			for _, category := range result.Categories {
				addGeoDataSuggestion(suggestions, file.Name, kind, category, "category", nil)
			}
			if result.Truncated {
				warnings = append(warnings, geoDataWarning(file.Name, fmt.Sprintf("results limited to %d categories", maxGeoDataSuggestions)))
			}
		case "domain":
			result, scanErr := searchGeoDataFileStream(ctx, path, kind, query.Value, maxGeoDataSuggestions)
			if scanErr != nil {
				if isGeoDataTimeout(scanErr) {
					writeJSON(w, http.StatusRequestTimeout, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Mode: query.Mode, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: "geodata suggestion search timed out"})
					return
				}
				scanWarnings = append(scanWarnings, geoDataWarning(file.Name, geoDataGenericFileError))
				continue
			}
			for _, category := range result.Categories {
				addGeoDataSuggestion(suggestions, file.Name, kind, category, "domain", []string{query.Value})
			}
		case "ip":
			result, scanErr := searchGeoDataFileStream(ctx, path, kind, query.Value, maxGeoDataSuggestions)
			if scanErr != nil {
				if isGeoDataTimeout(scanErr) {
					writeJSON(w, http.StatusRequestTimeout, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Mode: query.Mode, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: "geodata suggestion search timed out"})
					return
				}
				scanWarnings = append(scanWarnings, geoDataWarning(file.Name, geoDataGenericFileError))
				continue
			}
			for _, category := range result.Categories {
				addGeoDataSuggestion(suggestions, file.Name, kind, category, "ip", []string{query.Value})
			}
		case "dns":
			for _, ip := range resolved {
				result, scanErr := searchGeoDataFileStream(ctx, path, kind, ip, maxGeoDataSuggestions)
				if scanErr != nil {
					if isGeoDataTimeout(scanErr) {
						writeJSON(w, http.StatusRequestTimeout, geoDataSuggestResponse{Success: false, Kind: kind, Query: raw, Mode: query.Mode, Suggestions: []geoDataSuggestion{}, Mutation: "NONE", Error: "geodata suggestion search timed out"})
						return
					}
					scanWarnings = append(scanWarnings, geoDataWarning(file.Name, geoDataGenericFileError))
					break
				}
				for _, category := range result.Categories {
					addGeoDataSuggestion(suggestions, file.Name, kind, category, "dns", []string{ip})
				}
			}
		}
		if len(suggestions) >= maxGeoDataSuggestions {
			break
		}
	}

	items := make([]geoDataSuggestion, 0, len(suggestions))
	for _, item := range suggestions {
		item.Evidence = uniqueSortedStrings(item.Evidence, maxGeoDataDNSAnswers)
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].File < items[j].File
	})
	if len(items) > maxGeoDataSuggestions {
		items = items[:maxGeoDataSuggestions]
	}
	// A mixed GeoData directory is common on real XKeen installs. If at least one
	// compatible DAT produced usable suggestions, generic decode/type failures from
	// unrelated DAT files are diagnostic noise rather than a failed user lookup.
	// Keep safety/budget/truncation warnings, but surface generic scan failures only
	// when no usable suggestion was found.
	if len(items) == 0 {
		warnings = append(warnings, scanWarnings...)
	}

	writeJSON(w, http.StatusOK, geoDataSuggestResponse{
		Success: true, Kind: kind, Query: raw, Mode: query.Mode,
		Suggestions: items, Resolved: resolved, Warnings: warnings, Mutation: "NONE",
	})
}

func classifyGeoDataSuggestQuery(kind GeoDataKind, raw string) (geoDataSuggestQuery, error) {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)

	if strings.HasPrefix(lower, "geosite:") || strings.HasPrefix(lower, "geoip:") || strings.HasPrefix(lower, "ext:") {
		prefix, err := normalizeGeoDataCategoryPrefix(kind, value)
		if err != nil {
			return geoDataSuggestQuery{}, err
		}
		return geoDataSuggestQuery{Mode: "prefix", Value: prefix}, nil
	}

	if ip := net.ParseIP(value); ip != nil {
		if kind != GeoDataIP {
			return geoDataSuggestQuery{}, fmt.Errorf("GeoSite expects a domain, URL or category prefix")
		}
		return geoDataSuggestQuery{Mode: "ip", Value: ip.String()}, nil
	}

	if looksLikeGeoDataHost(value) {
		host, err := normalizeGeoDataHost(value)
		if err != nil {
			return geoDataSuggestQuery{}, err
		}
		if kind == GeoDataIP {
			return geoDataSuggestQuery{Mode: "dns", Value: host}, nil
		}
		return geoDataSuggestQuery{Mode: "domain", Value: host}, nil
	}

	prefix, err := normalizeGeoDataCategoryPrefix(kind, value)
	if err != nil {
		return geoDataSuggestQuery{}, err
	}
	return geoDataSuggestQuery{Mode: "prefix", Value: prefix}, nil
}

func looksLikeGeoDataHost(value string) bool {
	value = strings.TrimSpace(value)
	return strings.Contains(value, "://") || strings.Contains(value, "/") || strings.Contains(value, ".")
}

func normalizeGeoDataHost(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("invalid host")
	}
	candidate := value
	if !strings.Contains(candidate, "://") {
		candidate = "http://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid host or URL")
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(parsed.Hostname()), "."))
	if host == "" {
		return "", fmt.Errorf("invalid host or URL")
	}
	normalized, err := normalizePolicyDomain(host)
	if err != nil {
		return "", fmt.Errorf("invalid host or URL")
	}
	return normalized, nil
}

func normalizeGeoDataCategoryPrefix(kind GeoDataKind, raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(value, "geosite:"):
		if kind != GeoDataSite {
			return "", fmt.Errorf("GeoIP expects geoip category prefix, IP or host")
		}
		value = strings.TrimSpace(value[len("geosite:"):])
	case strings.HasPrefix(value, "geoip:"):
		if kind != GeoDataIP {
			return "", fmt.Errorf("GeoSite expects geosite category prefix or domain")
		}
		value = strings.TrimSpace(value[len("geoip:"):])
	case strings.HasPrefix(value, "ext:"):
		parts := strings.SplitN(value, ":", 3)
		if len(parts) != 3 || !validGeoDataFileSelector(parts[1]) {
			return "", fmt.Errorf("invalid GeoData ext selector prefix")
		}
		value = strings.TrimSpace(parts[2])
	}
	if value == "" || len(value) > 128 || !policyCategoryPattern.MatchString(value) {
		return "", fmt.Errorf("invalid GeoData category prefix")
	}
	return value, nil
}

func resolveGeoDataHost(parent context.Context, host string) ([]string, error) {
	ctx, cancel := context.WithTimeout(parent, geoDataDNSTimeout)
	defer cancel()
	addrs, err := geoDataLookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("не удалось разрешить %s через текущий DNS", host)
	}
	set := make(map[string]struct{}, len(addrs))
	for _, addr := range addrs {
		if addr.IP == nil {
			continue
		}
		set[addr.IP.String()] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for ip := range set {
		out = append(out, ip)
	}
	sort.Strings(out)
	if len(out) > maxGeoDataDNSAnswers {
		out = out[:maxGeoDataDNSAnswers]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("DNS не вернул A/AAAA для %s", host)
	}
	return out, nil
}

func addGeoDataSuggestion(dst map[string]*geoDataSuggestion, file string, kind GeoDataKind, category, match string, evidence []string) {
	category = strings.ToLower(strings.TrimSpace(category))
	if category == "" {
		return
	}
	key := file + "\x00" + string(kind) + "\x00" + category
	if existing := dst[key]; existing != nil {
		existing.Evidence = append(existing.Evidence, evidence...)
		return
	}
	selectorPrefix := string(kind) + ":"
	selector := selectorPrefix + category
	ext := "ext:" + file + ":" + category
	if (kind == GeoDataSite && !strings.EqualFold(file, "geosite.dat")) || (kind == GeoDataIP && !strings.EqualFold(file, "geoip.dat")) {
		selector = ext
	}
	dst[key] = &geoDataSuggestion{
		File: file, Kind: kind, Category: category,
		Selector: selector, ExtSelector: ext, Match: match,
		Evidence: append([]string(nil), evidence...),
	}
}

func uniqueSortedStrings(values []string, limit int) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func searchGeoDataCategoryPrefixFileStream(ctx context.Context, path string, expected GeoDataKind, prefix string, categoryLimit int) (geoDataStreamSearchResult, error) {
	if categoryLimit <= 0 {
		return geoDataStreamSearchResult{}, fmt.Errorf("invalid category limit")
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return geoDataStreamSearchResult{}, fmt.Errorf("invalid category prefix")
	}

	info, err := os.Lstat(path)
	if err != nil {
		return geoDataStreamSearchResult{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return geoDataStreamSearchResult{}, fmt.Errorf("not a regular geodata file")
	}
	if info.Size() < 0 || info.Size() > maxGeoDataFileSize {
		return geoDataStreamSearchResult{}, fmt.Errorf("geodata file exceeds safe size")
	}
	f, err := os.Open(path)
	if err != nil {
		return geoDataStreamSearchResult{}, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return geoDataStreamSearchResult{}, err
	}
	if !opened.Mode().IsRegular() || opened.Size() != info.Size() {
		return geoDataStreamSearchResult{}, fmt.Errorf("geodata file changed during open")
	}

	br := bufio.NewReaderSize(f, geoDataStreamBufferSize)
	found := make(map[string]struct{})
	var consumed int64
	var observedExpected bool
	var observedOther bool
	for consumed < opened.Size() {
		if err := geoDataContextErr(ctx); err != nil {
			return geoDataStreamSearchResult{}, err
		}
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return geoDataStreamSearchResult{}, err
		}
		consumed += n
		if wire != 2 {
			n, err := skipGeoFieldBody(ctx, br, wire, opened.Size()-consumed)
			if err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += n
			continue
		}
		length, n, err := readGeoUvarint(ctx, br)
		if err != nil {
			return geoDataStreamSearchResult{}, err
		}
		consumed += n
		if length > uint64(maxGeoDataStreamEntrySize) {
			return geoDataStreamSearchResult{}, fmt.Errorf("geodata entry exceeds safe size")
		}
		if err := ensureGeoRemaining(opened.Size(), consumed, int64(length)); err != nil {
			return geoDataStreamSearchResult{}, err
		}
		if number != 1 {
			if err := skipGeoBytes(ctx, br, int64(length)); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += int64(length)
			continue
		}
		code, observed, err := scanGeoDataCategoryEntryStream(ctx, br, int64(length))
		if err != nil {
			return geoDataStreamSearchResult{}, err
		}
		consumed += int64(length)
		if observed == expected {
			observedExpected = true
			if strings.HasPrefix(strings.ToLower(code), prefix) && code != "" {
				found[strings.ToLower(code)] = struct{}{}
				if len(found) >= categoryLimit {
					return geoDataStreamSearchResult{Categories: sortedStringSet(found), Truncated: true, Observed: true}, nil
				}
			}
		} else if observed != GeoDataUnknown {
			observedOther = true
		}
	}
	if !observedExpected {
		if observedOther {
			return geoDataStreamSearchResult{}, errGeoDataStreamKindMismatch
		}
		return geoDataStreamSearchResult{}, errGeoDataStreamKindMismatch
	}
	return geoDataStreamSearchResult{Categories: sortedStringSet(found), Observed: true}, nil
}

func scanGeoDataCategoryEntryStream(ctx context.Context, br *bufio.Reader, total int64) (string, GeoDataKind, error) {
	var consumed int64
	var code string
	observed := GeoDataUnknown
	for consumed < total {
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return "", GeoDataUnknown, err
		}
		consumed += n
		switch {
		case number == 1 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return "", GeoDataUnknown, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamValueSize) {
				return "", GeoDataUnknown, fmt.Errorf("geodata category exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return "", GeoDataUnknown, err
			}
			value, err := readGeoBytes(ctx, br, int64(length))
			if err != nil {
				return "", GeoDataUnknown, err
			}
			code = string(value)
			consumed += int64(length)
		case number == 2 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return "", GeoDataUnknown, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamNestedSize) {
				return "", GeoDataUnknown, fmt.Errorf("geodata nested entry exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return "", GeoDataUnknown, err
			}
			kind, err := scanGeoDataNestedKind(ctx, br, int64(length))
			if err != nil {
				return "", GeoDataUnknown, err
			}
			consumed += int64(length)
			if kind != GeoDataUnknown {
				if observed != GeoDataUnknown && observed != kind {
					return "", GeoDataUnknown, fmt.Errorf("mixed geodata entry types")
				}
				observed = kind
			}
		default:
			n, err := skipGeoFieldBody(ctx, br, wire, total-consumed)
			if err != nil {
				return "", GeoDataUnknown, err
			}
			consumed += n
		}
	}
	if consumed != total {
		return "", GeoDataUnknown, fmt.Errorf("invalid geodata entry length")
	}
	return code, observed, nil
}

func scanGeoDataNestedKind(ctx context.Context, br *bufio.Reader, total int64) (GeoDataKind, error) {
	var consumed int64
	var site bool
	var ip bool
	for consumed < total {
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return GeoDataUnknown, err
		}
		consumed += n
		switch {
		case number == 1 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return GeoDataUnknown, err
			}
			consumed += n
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return GeoDataUnknown, err
			}
			if length == net.IPv4len || length == net.IPv6len {
				ip = true
			}
			if err := skipGeoBytes(ctx, br, int64(length)); err != nil {
				return GeoDataUnknown, err
			}
			consumed += int64(length)
		case number == 2 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return GeoDataUnknown, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamValueSize) {
				return GeoDataUnknown, fmt.Errorf("geodata nested value exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return GeoDataUnknown, err
			}
			site = true
			if err := skipGeoBytes(ctx, br, int64(length)); err != nil {
				return GeoDataUnknown, err
			}
			consumed += int64(length)
		default:
			n, err := skipGeoFieldBody(ctx, br, wire, total-consumed)
			if err != nil {
				return GeoDataUnknown, err
			}
			consumed += n
		}
	}
	if consumed != total {
		return GeoDataUnknown, fmt.Errorf("invalid geodata nested length")
	}
	if site && ip {
		return GeoDataUnknown, fmt.Errorf("mixed geodata nested type")
	}
	if site {
		return GeoDataSite, nil
	}
	if ip {
		return GeoDataIP, nil
	}
	return GeoDataUnknown, nil
}
