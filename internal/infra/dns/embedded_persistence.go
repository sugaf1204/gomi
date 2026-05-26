package dns

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (s *EmbeddedServer) loadDynamicRecordsLocked() error {
	if s.dynamicRecordsPath == "" {
		return nil
	}
	loadedAt := nowUTC()
	data, err := os.ReadFile(s.dynamicRecordsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read dynamic dns records: %w", err)
	}
	if info, statErr := os.Stat(s.dynamicRecordsPath); statErr == nil {
		loadedAt = info.ModTime().UTC()
	}
	var file dynamicRecordFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse dynamic dns records: %w", err)
	}
	records := map[string]map[uint16]dynamicRecordSet{}
	for _, entry := range file.Records {
		name, ok := canonicalFQDN(entry.Name)
		if !ok {
			continue
		}
		rrType, ok := dynamicTypeFromName(entry.Type)
		if !ok {
			continue
		}
		ttl := entry.TTL
		if ttl == 0 {
			ttl = s.ttl
		}
		values := normalizeDynamicRecordValues(rrType, entry.Values)
		if len(values) == 0 {
			continue
		}
		createdAt := entry.CreatedAt.UTC()
		if createdAt.IsZero() {
			createdAt = loadedAt
		}
		updatedAt := entry.UpdatedAt.UTC()
		if updatedAt.IsZero() {
			updatedAt = loadedAt
		}
		if records[name] == nil {
			records[name] = map[uint16]dynamicRecordSet{}
		}
		records[name][rrType] = dynamicRecordSet{
			TTL:       ttl,
			Values:    values,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}
	}
	s.dynamicRecords = records
	return nil
}

func (s *EmbeddedServer) persistDynamicRecordsLocked() error {
	if s.dynamicRecordsPath == "" {
		return nil
	}
	file := dynamicRecordFile{Records: dynamicRecordEntries(s.dynamicRecords)}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dynamic dns records: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.dynamicRecordsPath), 0o755); err != nil {
		return fmt.Errorf("create dynamic dns record dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.dynamicRecordsPath), ".records-*.json")
	if err != nil {
		return fmt.Errorf("create dynamic dns record temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write dynamic dns records: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close dynamic dns records: %w", err)
	}
	if err := os.Rename(tmpName, s.dynamicRecordsPath); err != nil {
		return fmt.Errorf("replace dynamic dns records: %w", err)
	}
	return nil
}
