package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	defaultGeoDataAssetDir = "/opt/etc/xray/dat"
	maxGeoDataFileSize     = int64(96 << 20)
)

type GeoDataKind string

const (
	GeoDataUnknown GeoDataKind = "unknown"
	GeoDataSite    GeoDataKind = "geosite"
	GeoDataIP      GeoDataKind = "geoip"
)

type GeoDataFile struct {
	Name  string      `json:"name"`
	Kind  GeoDataKind `json:"kind"`
	Size  int64       `json:"size"`
	Error string      `json:"error,omitempty"`
}

type protoWireField struct {
	number  int
	wire    int
	bytes   []byte
	varint  uint64
}

// DiscoverGeoDataFiles inspects only regular local .dat files in assetDir.
// It never follows directory entries that are symlinks and never performs a
// network request. Per-file parse/read failures are reported on the file entry
// so one bad optional geodata file does not hide the other installed files.
func DiscoverGeoDataFiles(assetDir string) ([]GeoDataFile, error) {
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
		item := GeoDataFile{Name: entry.Name(), Kind: GeoDataUnknown, Size: info.Size()}
		data, err := readBoundedGeoDataFile(filepath.Join(assetDir, entry.Name()))
		if err != nil {
			item.Error = err.Error()
			files = append(files, item)
			continue
		}
		kind, err := DetectGeoDataKind(data)
		if err != nil {
			item.Error = err.Error()
		} else {
			item.Kind = kind
		}
		files = append(files, item)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files, nil
}

func readBoundedGeoDataFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular geodata file")
	}
	if info.Size() < 0 || info.Size() > maxGeoDataFileSize {
		return nil, fmt.Errorf("geodata file exceeds %d bytes", maxGeoDataFileSize)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxGeoDataFileSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxGeoDataFileSize {
		return nil, fmt.Errorf("geodata file exceeds %d bytes", maxGeoDataFileSize)
	}
	return data, nil
}

// DetectGeoDataKind identifies the Xray GeoSiteList/GeoIPList wire shape.
// The parser intentionally implements only the stable fields required by
// geodat.proto and skips unrelated protobuf fields.
func DetectGeoDataKind(data []byte) (GeoDataKind, error) {
	pos := 0
	found := GeoDataUnknown
	for pos < len(data) {
		field, err := nextProtoWireField(data, &pos)
		if err != nil {
			return GeoDataUnknown, err
		}
		if field.number != 1 || field.wire != 2 {
			continue
		}
		kind, err := detectGeoEntryKind(field.bytes)
		if err != nil {
			return GeoDataUnknown, err
		}
		if kind == GeoDataUnknown {
			continue
		}
		if found != GeoDataUnknown && found != kind {
			return GeoDataUnknown, fmt.Errorf("mixed geodata entry types")
		}
		found = kind
	}
	return found, nil
}

func detectGeoEntryKind(entry []byte) (GeoDataKind, error) {
	pos := 0
	for pos < len(entry) {
		field, err := nextProtoWireField(entry, &pos)
		if err != nil {
			return GeoDataUnknown, err
		}
		if field.number != 2 || field.wire != 2 {
			continue
		}
		childPos := 0
		for childPos < len(field.bytes) {
			child, err := nextProtoWireField(field.bytes, &childPos)
			if err != nil {
				return GeoDataUnknown, err
			}
			// Domain.value = field 2 / length-delimited.
			if child.number == 2 && child.wire == 2 {
				return GeoDataSite, nil
			}
			// CIDR.ip = field 1 / length-delimited with 4 or 16 raw bytes.
			if child.number == 1 && child.wire == 2 && (len(child.bytes) == net.IPv4len || len(child.bytes) == net.IPv6len) {
				return GeoDataIP, nil
			}
		}
	}
	return GeoDataUnknown, nil
}

// SearchGeoSiteData returns matching category codes. Full-domain matching
// follows Xray Domain semantics; discovery additionally allows a partial query
// to match a stored literal so the UI can find a category from "youtube" as
// well as from "www.youtube.com". Search is read-only and deterministic.
func SearchGeoSiteData(data []byte, query string) ([]string, error) {
	query = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(query), "."))
	if query == "" || len(query) > 253 || strings.ContainsAny(query, " /\\:@") {
		return nil, fmt.Errorf("invalid geosite query")
	}
	categories := make(map[string]struct{})
	pos := 0
	for pos < len(data) {
		field, err := nextProtoWireField(data, &pos)
		if err != nil {
			return nil, err
		}
		if field.number != 1 || field.wire != 2 {
			continue
		}
		code, domains, err := parseGeoSiteEntry(field.bytes)
		if err != nil {
			return nil, err
		}
		if code == "" {
			continue
		}
		for _, domain := range domains {
			match, err := geoDomainMatches(query, domain.domainType, domain.value)
			if err != nil {
				return nil, fmt.Errorf("geosite %s: %w", code, err)
			}
			if match {
				categories[strings.ToLower(code)] = struct{}{}
				break
			}
		}
	}
	return sortedStringSet(categories), nil
}

type geoDomainRule struct {
	domainType uint64
	value      string
}

func parseGeoSiteEntry(entry []byte) (string, []geoDomainRule, error) {
	var code string
	domains := make([]geoDomainRule, 0)
	pos := 0
	for pos < len(entry) {
		field, err := nextProtoWireField(entry, &pos)
		if err != nil {
			return "", nil, err
		}
		switch {
		case field.number == 1 && field.wire == 2:
			code = string(field.bytes)
		case field.number == 2 && field.wire == 2:
			rule, ok, err := parseGeoDomainRule(field.bytes)
			if err != nil {
				return "", nil, err
			}
			if ok {
				domains = append(domains, rule)
			}
		}
	}
	return code, domains, nil
}

func parseGeoDomainRule(data []byte) (geoDomainRule, bool, error) {
	rule := geoDomainRule{}
	var hasValue bool
	pos := 0
	for pos < len(data) {
		field, err := nextProtoWireField(data, &pos)
		if err != nil {
			return geoDomainRule{}, false, err
		}
		switch {
		case field.number == 1 && field.wire == 0:
			rule.domainType = field.varint
		case field.number == 2 && field.wire == 2:
			rule.value = strings.ToLower(string(field.bytes))
			hasValue = rule.value != ""
		}
	}
	if rule.domainType > 3 {
		return geoDomainRule{}, false, fmt.Errorf("unsupported domain type %d", rule.domainType)
	}
	return rule, hasValue, nil
}

func geoDomainMatches(query string, domainType uint64, value string) (bool, error) {
	value = strings.ToLower(value)
	if value == "" {
		return false, nil
	}
	switch domainType {
	case 0: // Substr
		return strings.Contains(query, value) || strings.Contains(value, query), nil
	case 1: // Regex
		re, err := regexp.Compile(value)
		if err != nil {
			return false, fmt.Errorf("invalid regex: %w", err)
		}
		return re.MatchString(query), nil
	case 2: // Domain/root domain
		return query == value || strings.HasSuffix(query, "."+value) || strings.Contains(value, query), nil
	case 3: // Full
		return query == value || strings.Contains(value, query), nil
	default:
		return false, fmt.Errorf("unsupported domain type %d", domainType)
	}
}

// SearchGeoIPData accepts a literal IPv4/IPv6 address. It does not resolve a
// domain through an external DNS service; that behavior, if ever needed, must
// be an explicit higher-level helper rather than a hidden search dependency.
func SearchGeoIPData(data []byte, literalIP string) ([]string, error) {
	target := net.ParseIP(strings.TrimSpace(literalIP))
	if target == nil {
		return nil, fmt.Errorf("invalid literal IP")
	}
	categories := make(map[string]struct{})
	pos := 0
	for pos < len(data) {
		field, err := nextProtoWireField(data, &pos)
		if err != nil {
			return nil, err
		}
		if field.number != 1 || field.wire != 2 {
			continue
		}
		code, cidrs, reverse, err := parseGeoIPEntry(field.bytes)
		if err != nil {
			return nil, err
		}
		if code == "" || len(cidrs) == 0 {
			continue
		}
		matched := false
		for _, cidr := range cidrs {
			if rawCIDRContains(cidr.ip, cidr.prefix, target) {
				matched = true
				break
			}
		}
		if reverse {
			matched = !matched
		}
		if matched {
			categories[strings.ToLower(code)] = struct{}{}
		}
	}
	return sortedStringSet(categories), nil
}

type geoCIDR struct {
	ip     []byte
	prefix uint64
}

func parseGeoIPEntry(entry []byte) (string, []geoCIDR, bool, error) {
	var code string
	var reverse bool
	cidrs := make([]geoCIDR, 0)
	pos := 0
	for pos < len(entry) {
		field, err := nextProtoWireField(entry, &pos)
		if err != nil {
			return "", nil, false, err
		}
		switch {
		case field.number == 1 && field.wire == 2:
			code = string(field.bytes)
		case field.number == 2 && field.wire == 2:
			cidr, ok, err := parseGeoCIDR(field.bytes)
			if err != nil {
				return "", nil, false, err
			}
			if ok {
				cidrs = append(cidrs, cidr)
			}
		case field.number == 3 && field.wire == 0:
			reverse = field.varint != 0
		}
	}
	return code, cidrs, reverse, nil
}

func parseGeoCIDR(data []byte) (geoCIDR, bool, error) {
	var cidr geoCIDR
	var hasIP bool
	pos := 0
	for pos < len(data) {
		field, err := nextProtoWireField(data, &pos)
		if err != nil {
			return geoCIDR{}, false, err
		}
		switch {
		case field.number == 1 && field.wire == 2:
			if len(field.bytes) != net.IPv4len && len(field.bytes) != net.IPv6len {
				return geoCIDR{}, false, fmt.Errorf("invalid CIDR address length %d", len(field.bytes))
			}
			cidr.ip = append([]byte(nil), field.bytes...)
			hasIP = true
		case field.number == 2 && field.wire == 0:
			cidr.prefix = field.varint
		}
	}
	if !hasIP {
		return geoCIDR{}, false, nil
	}
	bits := uint64(32)
	if len(cidr.ip) == net.IPv6len {
		bits = 128
	}
	if cidr.prefix > bits {
		return geoCIDR{}, false, fmt.Errorf("invalid CIDR prefix %d", cidr.prefix)
	}
	return cidr, true, nil
}

func rawCIDRContains(network []byte, prefix uint64, target net.IP) bool {
	var candidate []byte
	switch len(network) {
	case net.IPv4len:
		candidate = target.To4()
	case net.IPv6len:
		if target.To4() != nil {
			return false
		}
		candidate = target.To16()
	default:
		return false
	}
	if candidate == nil {
		return false
	}
	full := int(prefix / 8)
	rem := uint(prefix % 8)
	for i := 0; i < full; i++ {
		if candidate[i] != network[i] {
			return false
		}
	}
	if rem == 0 {
		return true
	}
	mask := byte(0xff << (8 - rem))
	return candidate[full]&mask == network[full]&mask
}

func nextProtoWireField(data []byte, pos *int) (protoWireField, error) {
	if pos == nil || *pos < 0 || *pos >= len(data) {
		return protoWireField{}, io.ErrUnexpectedEOF
	}
	key, n := binary.Uvarint(data[*pos:])
	if n <= 0 {
		return protoWireField{}, fmt.Errorf("invalid protobuf key varint")
	}
	*pos += n
	number := int(key >> 3)
	wire := int(key & 7)
	if number <= 0 {
		return protoWireField{}, fmt.Errorf("invalid protobuf field number")
	}
	field := protoWireField{number: number, wire: wire}
	switch wire {
	case 0:
		v, m := binary.Uvarint(data[*pos:])
		if m <= 0 {
			return protoWireField{}, fmt.Errorf("invalid protobuf varint")
		}
		*pos += m
		field.varint = v
	case 1:
		if len(data)-*pos < 8 {
			return protoWireField{}, io.ErrUnexpectedEOF
		}
		*pos += 8
	case 2:
		length, m := binary.Uvarint(data[*pos:])
		if m <= 0 {
			return protoWireField{}, fmt.Errorf("invalid protobuf length")
		}
		*pos += m
		remaining := len(data) - *pos
		if length > uint64(remaining) {
			return protoWireField{}, io.ErrUnexpectedEOF
		}
		end := *pos + int(length)
		field.bytes = data[*pos:end]
		*pos = end
	case 5:
		if len(data)-*pos < 4 {
			return protoWireField{}, io.ErrUnexpectedEOF
		}
		*pos += 4
	default:
		return protoWireField{}, fmt.Errorf("unsupported protobuf wire type %d", wire)
	}
	return field, nil
}

func sortedStringSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
