package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	v3BackupManifestName    = "manifest.json"
	v3BackupManifestVersion = 1
)

type v3BackupTrackedFile struct {
	Name string
	Dst  string
}

type v3BackupManifest struct {
	Version int             `json:"version"`
	Files   map[string]bool `json:"files"`
}

func (a *app) v3BackupTrackedFiles() []v3BackupTrackedFile {
	return []v3BackupTrackedFile{
		{Name: "freenet.conf", Dst: a.cfg.ConfigPath},
		{Name: "subscription.url", Dst: a.cfg.SubPath},
		{Name: "profile_filter.regex", Dst: a.cfg.FilterPath},
		{Name: "04_outbounds.json", Dst: a.cfg.OutPath},
	}
}

func v3WriteBackupManifest(dir string, manifest v3BackupManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return errors.New("cannot encode backup manifest")
	}
	if err := atomicWrite(filepath.Join(dir, v3BackupManifestName), append(data, '\n'), 0600); err != nil {
		return errors.New("cannot persist backup manifest")
	}
	return nil
}

func v3ReadBackupManifest(dir string) (v3BackupManifest, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, v3BackupManifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return v3BackupManifest{}, false, nil
		}
		return v3BackupManifest{}, false, errors.New("cannot read backup manifest")
	}
	var manifest v3BackupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return v3BackupManifest{}, true, errors.New("backup manifest is invalid")
	}
	if manifest.Version != v3BackupManifestVersion || manifest.Files == nil {
		return v3BackupManifest{}, true, errors.New("backup manifest is unsupported")
	}
	return manifest, true, nil
}

func v3CaptureBackupSnapshot(dir string, files []v3BackupTrackedFile) error {
	manifest := v3BackupManifest{
		Version: v3BackupManifestVersion,
		Files:   make(map[string]bool, len(files)),
	}
	for _, file := range files {
		data, err := os.ReadFile(file.Dst)
		if err != nil {
			if os.IsNotExist(err) {
				manifest.Files[file.Name] = false
				continue
			}
			return errors.New("cannot read tracked file for backup")
		}
		manifest.Files[file.Name] = true
		if err := atomicWrite(filepath.Join(dir, file.Name), data, 0600); err != nil {
			return errors.New("cannot persist tracked backup file")
		}
	}
	return v3WriteBackupManifest(dir, manifest)
}

func v3RestorePresentFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return errors.New("backup file is unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return errors.New("cannot prepare restore destination")
	}
	tmp := dst + ".restore-new"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return errors.New("cannot stage restored file")
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return errors.New("cannot commit restored file")
	}
	return nil
}

func v3ApplyBackupSnapshot(dir string, files []v3BackupTrackedFile, requireManifest bool) error {
	manifest, hasManifest, err := v3ReadBackupManifest(dir)
	if err != nil {
		return err
	}
	if requireManifest && !hasManifest {
		return errors.New("rollback snapshot manifest is unavailable")
	}
	for _, file := range files {
		src := filepath.Join(dir, file.Name)
		if hasManifest {
			present, ok := manifest.Files[file.Name]
			if !ok {
				return errors.New("backup manifest is incomplete")
			}
			if !present {
				if err := os.Remove(file.Dst); err != nil && !os.IsNotExist(err) {
					return errors.New("cannot restore previous file absence")
				}
				continue
			}
			if _, err := os.Stat(src); err != nil {
				return errors.New("backup manifest references unavailable file")
			}
			if err := v3RestorePresentFile(src, file.Dst); err != nil {
				return err
			}
			continue
		}

		// Legacy snapshots did not encode absence. Missing backup files therefore
		// retain the historical no-op meaning and must never cause deletion.
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return errors.New("cannot inspect legacy backup file")
		}
		if err := v3RestorePresentFile(src, file.Dst); err != nil {
			return err
		}
	}
	return nil
}

func v3VerifyBackupSnapshot(dir string, files []v3BackupTrackedFile, requireManifest bool) error {
	manifest, hasManifest, err := v3ReadBackupManifest(dir)
	if err != nil {
		return err
	}
	if requireManifest && !hasManifest {
		return errors.New("rollback snapshot manifest is unavailable")
	}
	for _, file := range files {
		src := filepath.Join(dir, file.Name)
		if hasManifest {
			present, ok := manifest.Files[file.Name]
			if !ok {
				return errors.New("backup manifest is incomplete")
			}
			if !present {
				if _, err := os.Stat(file.Dst); err == nil {
					return errors.New("restored file should be absent")
				} else if !os.IsNotExist(err) {
					return errors.New("cannot verify restored file absence")
				}
				continue
			}
		} else {
			if _, err := os.Stat(src); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return errors.New("cannot inspect legacy backup file")
			}
		}

		want, err := os.ReadFile(src)
		if err != nil {
			return errors.New("cannot read backup file for verification")
		}
		got, err := os.ReadFile(file.Dst)
		if err != nil {
			return errors.New("cannot read restored file for verification")
		}
		if !bytes.Equal(got, want) {
			return errors.New("restored file verification failed")
		}
	}
	return nil
}

// Test seams keep failure-path tests deterministic without weakening production logic.
var (
	v3ApplyBackupSnapshotForRestore  = v3ApplyBackupSnapshot
	v3VerifyBackupSnapshotForRestore = v3VerifyBackupSnapshot
)

func v3RollbackBackupSnapshot(dir string, files []v3BackupTrackedFile) error {
	if err := v3ApplyBackupSnapshotForRestore(dir, files, true); err != nil {
		return err
	}
	return v3VerifyBackupSnapshotForRestore(dir, files, true)
}
