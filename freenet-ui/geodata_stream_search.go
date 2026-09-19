package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	geoDataSearchTimeout          = 8 * time.Second
	maxGeoDataSearchTotalBytes   = int64(128 << 20)
	maxGeoDataStreamEntrySize    = int64(16 << 20)
	maxGeoDataStreamNestedSize   = int64(1 << 20)
	maxGeoDataStreamValueSize    = int64(64 << 10)
	geoDataStreamBufferSize      = 64 << 10
)

var errGeoDataStreamKindMismatch = errors.New("geodata content does not match requested kind")

type geoDataStreamSearchResult struct {
	Categories []string
	Truncated  bool
	Observed   bool
}

func searchGeoDataFileStream(ctx context.Context, path string, expected GeoDataKind, query string, categoryLimit int) (geoDataStreamSearchResult, error) {
	if categoryLimit <= 0 {
		return geoDataStreamSearchResult{}, fmt.Errorf("invalid category limit")
	}
	if err := validateGeoDataSearchQuery(expected, query); err != nil {
		return geoDataStreamSearchResult{}, err
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
	switch expected {
	case GeoDataSite:
		return searchGeoSiteStream(ctx, br, opened.Size(), query, categoryLimit)
	case GeoDataIP:
		target := net.ParseIP(strings.TrimSpace(query))
		if target == nil {
			return geoDataStreamSearchResult{}, fmt.Errorf("invalid literal IP")
		}
		return searchGeoIPStream(ctx, br, opened.Size(), target, categoryLimit)
	default:
		return geoDataStreamSearchResult{}, fmt.Errorf("unsupported geodata kind")
	}
}

func searchGeoSiteStream(ctx context.Context, br *bufio.Reader, total int64, query string, categoryLimit int) (geoDataStreamSearchResult, error) {
	query = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(query), "."))
	found := make(map[string]struct{})
	var consumed int64
	var observed bool

	for consumed < total {
		if err := geoDataContextErr(ctx); err != nil {
			return geoDataStreamSearchResult{}, err
		}
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return geoDataStreamSearchResult{}, err
		}
		consumed += n
		switch wire {
		case 0:
			_, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += n
		case 1:
			if err := ensureGeoRemaining(total, consumed, 8); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			if err := skipGeoBytes(ctx, br, 8); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += 8
		case 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamEntrySize) {
				return geoDataStreamSearchResult{}, fmt.Errorf("geodata entry exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			if number != 1 {
				if err := skipGeoBytes(ctx, br, int64(length)); err != nil {
					return geoDataStreamSearchResult{}, err
				}
				consumed += int64(length)
				continue
			}
			code, matched, entryObserved, err := scanGeoSiteEntryStream(ctx, br, int64(length), query)
			if err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += int64(length)
			observed = observed || entryObserved
			if matched && code != "" {
				found[strings.ToLower(code)] = struct{}{}
				if len(found) >= categoryLimit {
					return geoDataStreamSearchResult{Categories: sortedStringSet(found), Truncated: true, Observed: observed}, nil
				}
			}
		case 5:
			if err := ensureGeoRemaining(total, consumed, 4); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			if err := skipGeoBytes(ctx, br, 4); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += 4
		default:
			return geoDataStreamSearchResult{}, fmt.Errorf("unsupported protobuf wire type %d", wire)
		}
	}
	if consumed != total {
		return geoDataStreamSearchResult{}, io.ErrUnexpectedEOF
	}
	if !observed {
		return geoDataStreamSearchResult{}, errGeoDataStreamKindMismatch
	}
	return geoDataStreamSearchResult{Categories: sortedStringSet(found), Observed: true}, nil
}

func scanGeoSiteEntryStream(ctx context.Context, br *bufio.Reader, total int64, query string) (string, bool, bool, error) {
	var consumed int64
	var code string
	var matched bool
	var observed bool

	for consumed < total {
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return "", false, false, err
		}
		consumed += n
		switch {
		case number == 1 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return "", false, false, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamValueSize) {
				return "", false, false, fmt.Errorf("geodata category exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return "", false, false, err
			}
			value, err := readGeoBytes(ctx, br, int64(length))
			if err != nil {
				return "", false, false, err
			}
			code = string(value)
			consumed += int64(length)
		case number == 2 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return "", false, false, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamNestedSize) {
				return "", false, false, fmt.Errorf("geodata domain rule exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return "", false, false, err
			}
			ruleMatched, ruleObserved, err := scanGeoDomainRuleStream(ctx, br, int64(length), query)
			if err != nil {
				return "", false, false, err
			}
			consumed += int64(length)
			observed = observed || ruleObserved
			matched = matched || ruleMatched
		default:
			n, err := skipGeoFieldBody(ctx, br, wire, total-consumed)
			if err != nil {
				return "", false, false, err
			}
			consumed += n
		}
	}
	if consumed != total {
		return "", false, false, io.ErrUnexpectedEOF
	}
	return code, matched, observed, nil
}

func scanGeoDomainRuleStream(ctx context.Context, br *bufio.Reader, total int64, query string) (bool, bool, error) {
	var consumed int64
	var domainType uint64
	var value string
	var hasValue bool

	for consumed < total {
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return false, false, err
		}
		consumed += n
		switch {
		case number == 1 && wire == 0:
			v, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return false, false, err
			}
			domainType = v
			consumed += n
		case number == 2 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return false, false, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamValueSize) {
				return false, false, fmt.Errorf("geodata domain value exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return false, false, err
			}
			b, err := readGeoBytes(ctx, br, int64(length))
			if err != nil {
				return false, false, err
			}
			consumed += int64(length)
			value = strings.ToLower(string(b))
			hasValue = value != ""
		default:
			n, err := skipGeoFieldBody(ctx, br, wire, total-consumed)
			if err != nil {
				return false, false, err
			}
			consumed += n
		}
	}
	if consumed != total {
		return false, false, io.ErrUnexpectedEOF
	}
	if !hasValue {
		return false, false, nil
	}
	if domainType > 3 {
		return false, false, fmt.Errorf("unsupported domain type %d", domainType)
	}
	matched, err := geoDomainMatches(query, domainType, value)
	return matched, true, err
}

func searchGeoIPStream(ctx context.Context, br *bufio.Reader, total int64, target net.IP, categoryLimit int) (geoDataStreamSearchResult, error) {
	found := make(map[string]struct{})
	var consumed int64
	var observed bool

	for consumed < total {
		if err := geoDataContextErr(ctx); err != nil {
			return geoDataStreamSearchResult{}, err
		}
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return geoDataStreamSearchResult{}, err
		}
		consumed += n
		switch wire {
		case 0:
			_, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += n
		case 1:
			if err := ensureGeoRemaining(total, consumed, 8); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			if err := skipGeoBytes(ctx, br, 8); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += 8
		case 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamEntrySize) {
				return geoDataStreamSearchResult{}, fmt.Errorf("geodata entry exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			if number != 1 {
				if err := skipGeoBytes(ctx, br, int64(length)); err != nil {
					return geoDataStreamSearchResult{}, err
				}
				consumed += int64(length)
				continue
			}
			code, matched, entryObserved, err := scanGeoIPEntryStream(ctx, br, int64(length), target)
			if err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += int64(length)
			observed = observed || entryObserved
			if matched && code != "" {
				found[strings.ToLower(code)] = struct{}{}
				if len(found) >= categoryLimit {
					return geoDataStreamSearchResult{Categories: sortedStringSet(found), Truncated: true, Observed: observed}, nil
				}
			}
		case 5:
			if err := ensureGeoRemaining(total, consumed, 4); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			if err := skipGeoBytes(ctx, br, 4); err != nil {
				return geoDataStreamSearchResult{}, err
			}
			consumed += 4
		default:
			return geoDataStreamSearchResult{}, fmt.Errorf("unsupported protobuf wire type %d", wire)
		}
	}
	if consumed != total {
		return geoDataStreamSearchResult{}, io.ErrUnexpectedEOF
	}
	if !observed {
		return geoDataStreamSearchResult{}, errGeoDataStreamKindMismatch
	}
	return geoDataStreamSearchResult{Categories: sortedStringSet(found), Observed: true}, nil
}

func scanGeoIPEntryStream(ctx context.Context, br *bufio.Reader, total int64, target net.IP) (string, bool, bool, error) {
	var consumed int64
	var code string
	var reverse bool
	var matchedAny bool
	var observed bool

	for consumed < total {
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return "", false, false, err
		}
		consumed += n
		switch {
		case number == 1 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return "", false, false, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamValueSize) {
				return "", false, false, fmt.Errorf("geodata category exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return "", false, false, err
			}
			value, err := readGeoBytes(ctx, br, int64(length))
			if err != nil {
				return "", false, false, err
			}
			code = string(value)
			consumed += int64(length)
		case number == 2 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return "", false, false, err
			}
			consumed += n
			if length > uint64(maxGeoDataStreamNestedSize) {
				return "", false, false, fmt.Errorf("geodata CIDR exceeds safe size")
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return "", false, false, err
			}
			contains, cidrObserved, err := scanGeoCIDRStream(ctx, br, int64(length), target)
			if err != nil {
				return "", false, false, err
			}
			consumed += int64(length)
			observed = observed || cidrObserved
			matchedAny = matchedAny || contains
		case number == 3 && wire == 0:
			v, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return "", false, false, err
			}
			reverse = v != 0
			consumed += n
		default:
			n, err := skipGeoFieldBody(ctx, br, wire, total-consumed)
			if err != nil {
				return "", false, false, err
			}
			consumed += n
		}
	}
	if consumed != total {
		return "", false, false, io.ErrUnexpectedEOF
	}
	if !observed {
		return code, false, false, nil
	}
	matched := matchedAny
	if reverse {
		matched = !matched
	}
	return code, matched, true, nil
}

func scanGeoCIDRStream(ctx context.Context, br *bufio.Reader, total int64, target net.IP) (bool, bool, error) {
	var consumed int64
	var rawIP []byte
	var prefix uint64
	var hasIP bool

	for consumed < total {
		number, wire, n, err := readGeoProtoKey(ctx, br)
		if err != nil {
			return false, false, err
		}
		consumed += n
		switch {
		case number == 1 && wire == 2:
			length, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return false, false, err
			}
			consumed += n
			if length != net.IPv4len && length != net.IPv6len {
				return false, false, fmt.Errorf("invalid CIDR address length %d", length)
			}
			if err := ensureGeoRemaining(total, consumed, int64(length)); err != nil {
				return false, false, err
			}
			rawIP, err = readGeoBytes(ctx, br, int64(length))
			if err != nil {
				return false, false, err
			}
			hasIP = true
			consumed += int64(length)
		case number == 2 && wire == 0:
			v, n, err := readGeoUvarint(ctx, br)
			if err != nil {
				return false, false, err
			}
			prefix = v
			consumed += n
		default:
			n, err := skipGeoFieldBody(ctx, br, wire, total-consumed)
			if err != nil {
				return false, false, err
			}
			consumed += n
		}
	}
	if consumed != total {
		return false, false, io.ErrUnexpectedEOF
	}
	if !hasIP {
		return false, false, nil
	}
	bits := uint64(32)
	if len(rawIP) == net.IPv6len {
		bits = 128
	}
	if prefix > bits {
		return false, false, fmt.Errorf("invalid CIDR prefix %d", prefix)
	}
	return rawCIDRContains(rawIP, prefix, target), true, nil
}

func readGeoProtoKey(ctx context.Context, br *bufio.Reader) (int, int, int64, error) {
	key, n, err := readGeoUvarint(ctx, br)
	if err != nil {
		return 0, 0, n, err
	}
	number := int(key >> 3)
	wire := int(key & 7)
	if number <= 0 {
		return 0, 0, n, fmt.Errorf("invalid protobuf field number")
	}
	return number, wire, n, nil
}

func readGeoUvarint(ctx context.Context, br *bufio.Reader) (uint64, int64, error) {
	var value uint64
	for i := 0; i < binary.MaxVarintLen64; i++ {
		if err := geoDataContextErr(ctx); err != nil {
			return 0, int64(i), err
		}
		b, err := br.ReadByte()
		if err != nil {
			return 0, int64(i), err
		}
		if b < 0x80 {
			if i == binary.MaxVarintLen64-1 && b > 1 {
				return 0, int64(i + 1), fmt.Errorf("protobuf varint overflow")
			}
			return value | uint64(b)<<uint(7*i), int64(i + 1), nil
		}
		value |= uint64(b&0x7f) << uint(7*i)
	}
	return 0, binary.MaxVarintLen64, fmt.Errorf("protobuf varint overflow")
}

func skipGeoFieldBody(ctx context.Context, br *bufio.Reader, wire int, remaining int64) (int64, error) {
	switch wire {
	case 0:
		_, n, err := readGeoUvarint(ctx, br)
		return n, err
	case 1:
		if err := ensureGeoRemaining(remaining, 0, 8); err != nil {
			return 0, err
		}
		return 8, skipGeoBytes(ctx, br, 8)
	case 2:
		length, n, err := readGeoUvarint(ctx, br)
		if err != nil {
			return n, err
		}
		if length > uint64(maxGeoDataStreamEntrySize) {
			return n, fmt.Errorf("protobuf field exceeds safe size")
		}
		if err := ensureGeoRemaining(remaining, n, int64(length)); err != nil {
			return n, err
		}
		if err := skipGeoBytes(ctx, br, int64(length)); err != nil {
			return n, err
		}
		return n + int64(length), nil
	case 5:
		if err := ensureGeoRemaining(remaining, 0, 4); err != nil {
			return 0, err
		}
		return 4, skipGeoBytes(ctx, br, 4)
	default:
		return 0, fmt.Errorf("unsupported protobuf wire type %d", wire)
	}
}

func readGeoBytes(ctx context.Context, br *bufio.Reader, n int64) ([]byte, error) {
	if n < 0 || n > maxGeoDataStreamValueSize {
		return nil, fmt.Errorf("protobuf value exceeds safe size")
	}
	out := make([]byte, int(n))
	if err := readGeoExact(ctx, br, out); err != nil {
		return nil, err
	}
	return out, nil
}

func skipGeoBytes(ctx context.Context, br *bufio.Reader, n int64) error {
	var scratch [32 << 10]byte
	for n > 0 {
		if err := geoDataContextErr(ctx); err != nil {
			return err
		}
		chunk := int64(len(scratch))
		if n < chunk {
			chunk = n
		}
		if err := readGeoExact(ctx, br, scratch[:int(chunk)]); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}

func readGeoExact(ctx context.Context, br *bufio.Reader, out []byte) error {
	for len(out) > 0 {
		if err := geoDataContextErr(ctx); err != nil {
			return err
		}
		n, err := br.Read(out)
		if n > 0 {
			out = out[n:]
		}
		if err != nil {
			if err == io.EOF && len(out) == 0 {
				return nil
			}
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

func ensureGeoRemaining(total, consumed, need int64) error {
	if total < 0 || consumed < 0 || need < 0 || consumed > total || need > total-consumed {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func geoDataContextErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func sortGeoDataMatches(matches []geoDataSearchMatch) {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].File != matches[j].File {
			return matches[i].File < matches[j].File
		}
		return matches[i].Kind < matches[j].Kind
	})
}

func geoDataWarning(file, reason string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return geoDataGenericFileError
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = geoDataGenericFileError
	}
	return file + ": " + reason
}

func isGeoDataTimeout(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func geoDataSearchContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, geoDataSearchTimeout)
}

func validateGeoDataStreamRegex(value string) error {
	if len(value) > int(maxGeoDataStreamValueSize) {
		return fmt.Errorf("regex exceeds safe size")
	}
	_, err := regexp.Compile(value)
	return err
}
