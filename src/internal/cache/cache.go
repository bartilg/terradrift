package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"terradrift/src/internal/model"
)

const DefaultCachePath = ".terradrift/last.json"

func Save(report model.ScanReport, path string) error {
	if path == "" {
		path = DefaultCachePath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create cache file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode cache json: %w", err)
	}
	return nil
}

func Load(path string) (model.ScanReport, error) {
	if path == "" {
		path = DefaultCachePath
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return model.ScanReport{}, err
	}
	var report model.ScanReport
	if err := json.Unmarshal(b, &report); err != nil {
		return model.ScanReport{}, err
	}
	return report, nil
}
