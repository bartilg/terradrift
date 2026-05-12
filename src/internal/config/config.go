package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"terradrift/src/internal/model"

	"gopkg.in/yaml.v3"
)

type FileConfig struct {
	EnvFile        string   `yaml:"env_file"`
	Project        string   `yaml:"project"`
	Path           string   `yaml:"path"`
	Format         string   `yaml:"format"`
	Output         string   `yaml:"output"`
	FailOn         string   `yaml:"fail_on"`
	ResourceTypes  []string `yaml:"resource_types"`
	IgnoreDefaults bool     `yaml:"ignore_defaults"`
	Debug          bool     `yaml:"debug"`
}

type ScanOptions struct {
	Path           string
	ConfigPath     string
	Project        string
	Format         string
	Output         string
	FailOn         string
	ResourceTypes  []string
	IgnoreDefaults bool
	Debug          bool
}

var DefaultResourceTypes = []string{
	model.ResourceTypeArtifactRegistryRepository,
	model.ResourceTypeBigQueryDataset,
	model.ResourceTypeBucket,
	model.ResourceTypeCloudRunService,
	model.ResourceTypeComputeInstance,
	model.ResourceTypeComputeNetwork,
	model.ResourceTypeComputeSubnetwork,
	model.ResourceTypePubSubTopic,
	model.ResourceTypeSecretManagerSecret,
	model.ResourceTypeServiceAccount,
}

func DefaultScanOptions() ScanOptions {
	return ScanOptions{
		Path:          ".",
		Format:        "text",
		FailOn:        "never",
		ResourceTypes: append([]string{}, DefaultResourceTypes...),
	}
}

func LoadFile(path string) (FileConfig, error) {
	if strings.TrimSpace(path) == "" {
		return FileConfig{}, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return FileConfig{}, err
	}
	b, err := os.ReadFile(absPath)
	if err != nil {
		return FileConfig{}, err
	}

	var probe struct {
		EnvFile string `yaml:"env_file"`
	}
	if err := yaml.Unmarshal(b, &probe); err != nil {
		return FileConfig{}, err
	}

	envValues, err := loadConfigEnv(filepath.Dir(absPath), probe.EnvFile)
	if err != nil {
		return FileConfig{}, err
	}
	expanded := os.Expand(string(b), func(key string) string {
		if v, ok := envValues[key]; ok {
			return v
		}
		return ""
	})

	var cfg FileConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return FileConfig{}, err
	}
	var compat struct {
		IgnoreDefaults bool `yaml:"ignore-defaults"`
	}
	if err := yaml.Unmarshal([]byte(expanded), &compat); err != nil {
		return FileConfig{}, err
	}
	if compat.IgnoreDefaults {
		cfg.IgnoreDefaults = true
	}
	if cfg.EnvFile == "" {
		if probe.EnvFile != "" {
			cfg.EnvFile = probe.EnvFile
		} else {
			cfg.EnvFile = ".env"
		}
	}
	return cfg, nil
}

func Merge(defaults ScanOptions, fileCfg FileConfig, cli ScanOptions) (ScanOptions, error) {
	m := defaults
	pathSetByCLI := false

	if fileCfg.Project != "" {
		m.Project = fileCfg.Project
	}
	if fileCfg.Path != "" {
		m.Path = fileCfg.Path
	}
	if fileCfg.Format != "" {
		m.Format = fileCfg.Format
	}
	if fileCfg.Output != "" {
		m.Output = fileCfg.Output
	}
	if fileCfg.FailOn != "" {
		m.FailOn = fileCfg.FailOn
	}
	if len(fileCfg.ResourceTypes) > 0 {
		m.ResourceTypes = append([]string{}, fileCfg.ResourceTypes...)
	}
	if fileCfg.IgnoreDefaults {
		m.IgnoreDefaults = true
	}
	if fileCfg.Debug {
		m.Debug = true
	}

	if cli.Project != "" {
		m.Project = cli.Project
	}
	if cli.Path != "" && cli.Path != "." {
		m.Path = cli.Path
		pathSetByCLI = true
	}
	if cli.Format != "" && cli.Format != defaults.Format {
		m.Format = cli.Format
	}
	if cli.Output != "" {
		m.Output = cli.Output
	}
	if cli.FailOn != "" && cli.FailOn != defaults.FailOn {
		m.FailOn = cli.FailOn
	}
	if len(cli.ResourceTypes) > 0 {
		m.ResourceTypes = append([]string{}, cli.ResourceTypes...)
	}
	if cli.IgnoreDefaults {
		m.IgnoreDefaults = true
	}
	if cli.Debug {
		m.Debug = true
	}
	if cli.ConfigPath != "" {
		m.ConfigPath = cli.ConfigPath
	}
	if m.ConfigPath != "" && !filepath.IsAbs(m.ConfigPath) {
		absCfg, err := filepath.Abs(m.ConfigPath)
		if err != nil {
			return ScanOptions{}, err
		}
		m.ConfigPath = absCfg
	}

	if !pathSetByCLI && m.ConfigPath != "" && !filepath.IsAbs(m.Path) {
		m.Path = filepath.Join(filepath.Dir(m.ConfigPath), m.Path)
	}

	absPath, err := filepath.Abs(m.Path)
	if err != nil {
		return ScanOptions{}, err
	}
	m.Path = absPath

	m.Format = strings.ToLower(strings.TrimSpace(m.Format))
	if m.Format != "text" && m.Format != "json" {
		return ScanOptions{}, fmt.Errorf("invalid --format value: %s", m.Format)
	}

	m.FailOn = strings.ToLower(strings.TrimSpace(m.FailOn))
	switch m.FailOn {
	case "high", "medium", "any", "never":
	default:
		return ScanOptions{}, fmt.Errorf("invalid --fail-on value: %s", m.FailOn)
	}

	if len(m.ResourceTypes) == 0 {
		return ScanOptions{}, errors.New("resource_types cannot be empty")
	}
	clean := make([]string, 0, len(m.ResourceTypes))
	seen := map[string]struct{}{}
	for _, rt := range m.ResourceTypes {
		r := strings.TrimSpace(rt)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		clean = append(clean, r)
	}
	sort.Strings(clean)
	m.ResourceTypes = clean
	if len(m.ResourceTypes) == 0 {
		return ScanOptions{}, errors.New("resource_types cannot be empty after normalization")
	}

	return m, nil
}

func ParseResourceTypesArg(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func ConfigTemplate() string {
	return `env_file: ".env"
project: "${GCP_PROJECT_ID}"
path: "."
format: "text"
output: ""
fail_on: "never"
resource_types:
  - google_artifact_registry_repository
  - google_bigquery_dataset
  - google_compute_instance
  - google_compute_network
  - google_compute_subnetwork
  - google_cloud_run_v2_service
  - google_pubsub_topic
  - google_secret_manager_secret
  - google_storage_bucket
  - google_service_account
ignore_defaults: false
debug: false
`
}

func ResolveConfigPath(explicitPath string, pathHint string) (string, error) {
	if strings.TrimSpace(explicitPath) != "" {
		return filepath.Abs(explicitPath)
	}

	if strings.TrimSpace(pathHint) == "" {
		pathHint = "."
	}
	candidates := []string{
		filepath.Join(pathHint, "terradrift.yaml"),
		filepath.Join(pathHint, "terradrift.yml"),
		"terradrift.yaml",
		"terradrift.yml",
	}

	seen := map[string]struct{}{}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			return "", err
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", nil
}

func loadConfigEnv(configDir string, declaredEnvFile string) (map[string]string, error) {
	envFile := strings.TrimSpace(declaredEnvFile)
	explicitEnvFile := envFile != ""
	if envFile == "" {
		envFile = ".env"
	}

	if !filepath.IsAbs(envFile) {
		envFile = filepath.Join(configDir, envFile)
	}

	envValues := map[string]string{}
	fileValues, err := parseDotEnvFile(envFile)
	if err != nil {
		if os.IsNotExist(err) && !explicitEnvFile {
			fileValues = map[string]string{}
		} else {
			return nil, err
		}
	}
	for k, v := range fileValues {
		envValues[k] = v
	}
	for _, pair := range os.Environ() {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		envValues[k] = v
	}
	return envValues, nil
}

func parseDotEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf(".env parse error at %s:%d", path, lineNo)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return nil, fmt.Errorf(".env parse error at %s:%d: empty key", path, lineNo)
		}
		values[key] = normalizeDotEnvValue(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func normalizeDotEnvValue(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	if idx := strings.Index(v, " #"); idx >= 0 {
		return strings.TrimSpace(v[:idx])
	}
	return v
}
