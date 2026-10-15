package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// The technical archive is independent from the bounded UI/source logs. Only
// significant records reach this asynchronous spool; the quota protects USB
// storage and a reserved critical segment protects terminal failure/rollback
// evidence from routine event storms.
const (
	technicalJournalRetentionDays = 30
	technicalJournalDailyQuota int64 = 2 * 1024 * 1024
	technicalJournalCriticalQuota int64 = 512 * 1024
)

var technicalJournalLastPruneUTC atomic.Int64

type technicalDayFile struct {
	Path string
	Day time.Time
	Critical bool
}

func technicalJournalStartUTC(now time.Time) time.Time {
	// Retain the full rolling 30*24h interval. Its first and last instants
	// normally occupy 31 distinct UTC calendar files.
	return now.UTC().Add(-time.Duration(technicalJournalRetentionDays) * 24 * time.Hour)
}

func technicalJournalObservedTime(record technicalJournalRecord) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(record.ObservedAt))
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func technicalJournalDailyFilename(base string, at time.Time) string {
	return base + "." + at.UTC().Format("2006-01-02")
}

func technicalJournalDailyFiles(base string, now time.Time) ([]technicalDayFile, []string) {
	dir := filepath.Dir(base)
	name := filepath.Base(base) + "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	cutoff := technicalJournalStartUTC(now).Truncate(24 * time.Hour)
	end := now.UTC().Truncate(24 * time.Hour)
	files := make([]technicalDayFile, 0, technicalJournalRetentionDays*2)
	truncated := []string{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		suffix := strings.TrimPrefix(entry.Name(), name)
		if len(suffix) < 10 || !strings.HasPrefix(entry.Name(), name) {
			continue
		}
		day, err := time.Parse("2006-01-02", suffix[:10])
		if err != nil || day.Before(cutoff) || day.After(end) {
			continue
		}
		kind := suffix[10:]
		switch kind {
		case "", ".critical":
			files = append(files, technicalDayFile{
				Path: filepath.Join(dir, entry.Name()), Day: day, Critical: kind == ".critical",
			})
		case ".truncated":
			truncated = append(truncated, suffix[:10])
		}
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Day.Equal(files[j].Day) {
			return !files[i].Critical && files[j].Critical
		}
		return files[i].Day.Before(files[j].Day)
	})
	sort.Strings(truncated)
	return files, truncated
}

func pruneTechnicalJournalDays(base string, now time.Time) error {
	dir := filepath.Dir(base)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	prefix := filepath.Base(base) + "."
	cutoff := technicalJournalStartUTC(now).Truncate(24 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		suffix := strings.TrimPrefix(entry.Name(), prefix)
		if len(suffix) < 10 {
			continue
		}
		day, err := time.Parse("2006-01-02", suffix[:10])
		if err != nil || !day.Before(cutoff) {
			continue
		}
		switch suffix[10:] {
		case "", ".critical", ".truncated":
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func technicalJournalMaybePrune(base string, now time.Time) {
	utcDay := now.UTC().Unix() / 86400
	prev := technicalJournalLastPruneUTC.Load()
	if prev == utcDay || !technicalJournalLastPruneUTC.CompareAndSwap(prev, utcDay) {
		return
	}
	if err := pruneTechnicalJournalDays(base, now); err != nil {
		technicalJournalWriteErrors.Add(1)
	}
}

func appendTechnicalJournalCapped(path string, payload []byte, quota int64) error {
	existing, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	size := int64(0)
	if err == nil {
		size = existing.Size()
	}
	if size+int64(len(payload)) > quota {
		return errors.New("daily technical archive quota reached")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(payload)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func appendTechnicalJournalDaily(base string, record technicalJournalRecord, payload []byte) error {
	now := time.Now().UTC()
	at, ok := technicalJournalObservedTime(record)
	if !ok {
		at = now
	}
	// A broken or manipulated wall clock must not create unbounded historical
	// files or surprise dates. Such observations are still stored with their
	// original timestamp, but under the current UTC day.
	if at.Before(technicalJournalStartUTC(now)) || at.After(now.Add(1*time.Hour)) {
		at = now
	}
	if err := os.MkdirAll(filepath.Dir(base), 0700); err != nil {
		return err
	}
	technicalJournalMaybePrune(base, now)
	filename := technicalJournalDailyFilename(base, at)
	if err := appendTechnicalJournalCapped(filename, payload, technicalJournalDailyQuota); err == nil {
		return nil
	} else if err.Error() != "daily technical archive quota reached" {
		return err
	}
	// Critical incident and rollback results must not be crowded out by less
	// important repeated stages. Their reserved quota is capped separately.
	if record.Severity == "critical" {
		if err := appendTechnicalJournalCapped(filename+".critical", payload, technicalJournalCriticalQuota); err == nil {
			return nil
		}
	}
	// A visible per-day gap marker survives restart and is included in the
	// 30-day export manifest. Never fabricate complete coverage.
	_ = os.WriteFile(filename+".truncated", []byte("Daily journal limit reached; event coverage incomplete.\n"), 0600)
	return errors.New("technical journal day quota exceeded; archive gap recorded")
}

func technicalJournalReadPaths(base string, now time.Time) []string {
	days, _ := technicalJournalDailyFiles(base, now)
	paths := []string{base + ".1", base} // historical pre-0.8 files
	for _, item := range days {
		paths = append(paths, item.Path)
	}
	return paths
}

func sanitizeTechnicalJournalRecord(record technicalJournalRecord) technicalJournalRecord {
	record.ObservedAt = journalDiagnosticRedact(record.ObservedAt)
	record.EventAt = journalDiagnosticRedact(record.EventAt)
	record.Source = journalDiagnosticRedact(record.Source)
	record.Kind = journalDiagnosticRedact(record.Kind)
	record.Stage = journalDiagnosticRedact(record.Stage)
	record.Result = journalDiagnosticRedact(record.Result)
	record.Severity = journalDiagnosticRedact(record.Severity)
	record.Message = journalDiagnosticRedact(record.Message)
	record.Incident = journalDiagnosticRedact(record.Incident)
	record.State.AutoResult = journalDiagnosticRedact(record.State.AutoResult)
	record.State.RollbackReady = journalDiagnosticRedact(record.State.RollbackReady)
	record.State.HealthResult = journalDiagnosticRedact(record.State.HealthResult)
	return record
}

func technicalJournalRecordInWindow(record technicalJournalRecord, now time.Time) bool {
	at, ok := technicalJournalObservedTime(record)
	return ok && !at.Before(technicalJournalStartUTC(now)) && !at.After(now.Add(time.Hour))
}

// This endpoint is read-only: no networking, VPN/Xray work, jobs or mutations.
// gzip JSONL is streamed in bounded memory even on a 512 MiB router.
func (a *app) handleTechnicalJournalArchive(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC()
	base := technicalJournalPath()
	days, truncated := technicalJournalDailyFiles(base, now)
	paths := technicalJournalReadPaths(base, now)
	availableDays := map[string]bool{}
	for _, day := range days {
		availableDays[day.Day.Format("2006-01-02")] = true
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"freenet-technical-30d-%s.jsonl.gz\"", now.Format("20060102")))
	w.WriteHeader(http.StatusOK)
	archive := gzip.NewWriter(w)
	defer archive.Close()
	encoder := json.NewEncoder(archive)
	// The manifest makes missing history/truncation explicit; 30-day retention
	// does not claim that the router ran or logged successfully on every day.
	if err := encoder.Encode(map[string]any{
		"type": "manifest",
		"schema": 2,
		"generated_at": now.Format(time.RFC3339),
		"requested_days": technicalJournalRetentionDays,
		"start_utc": technicalJournalStartUTC(now).Format("2006-01-02"),
		"end_utc": now.Format("2006-01-02"),
		"available_daily_files": len(availableDays),
		"truncated_days": truncated,
		"queue_dropped_since_start": technicalJournalDropped.Load(),
		"writer_errors_since_start": technicalJournalWriteErrors.Load(),
		"read_only": true,
		"notes": "Legacy sources may be shorter; gaps mean no recorded events or unavailable history, never assumed healthy.",
	}); err != nil {
		return
	}
	for _, filename := range paths {
		file, err := os.Open(filename)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 32*1024)
		for scanner.Scan() {
			var record technicalJournalRecord
			if json.Unmarshal(scanner.Bytes(), &record) != nil || !technicalJournalRecordInWindow(record, now) {
				continue
			}
			if encoder.Encode(sanitizeTechnicalJournalRecord(record)) != nil {
				_ = file.Close()
				return
			}
		}
		_ = file.Close()
	}
}
