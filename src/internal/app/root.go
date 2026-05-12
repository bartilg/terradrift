package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"terradrift/src/internal/cache"
	"terradrift/src/internal/config"
	"terradrift/src/internal/model"
	"terradrift/src/internal/report"
	"terradrift/src/internal/scan"

	"github.com/spf13/cobra"
)

type Deps struct {
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
	Commit  string
	Date    string

	Observer scan.Observer
}

func Execute(deps Deps) int {
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}

	cmd := NewRootCmd(deps)
	err := cmd.Execute()
	if err == nil {
		return 0
	}
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		if err.Error() != "" {
			_, _ = fmt.Fprintln(deps.Stderr, err.Error())
		}
		return ee.ExitCode()
	}
	_, _ = fmt.Fprintln(deps.Stderr, err.Error())
	return 1
}

func NewRootCmd(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "terradrift",
		Short:         "Terradrift scans GCP resources against Terraform intent",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newScanCmd(deps))
	root.AddCommand(newExplainCmd(deps))
	root.AddCommand(newConfigCmd(deps))
	root.AddCommand(newVersionCmd(deps))

	return root
}

func newScanCmd(deps Deps) *cobra.Command {
	var (
		flagPath           string
		flagConfig         string
		flagProject        string
		flagFormat         string
		flagOutput         string
		flagFailOn         string
		flagResourceTypes  string
		flagIgnoreDefaults bool
		flagDebug          bool
	)

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan Terraform intent against GCP observed state",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedConfigPath, err := config.ResolveConfigPath(flagConfig, flagPath)
			if err != nil {
				return ExitError{Code: 1, Err: fmt.Errorf("resolve config: %w", err)}
			}

			defaults := config.DefaultScanOptions()
			cliOpts := config.ScanOptions{
				Path:           flagPath,
				ConfigPath:     resolvedConfigPath,
				Project:        flagProject,
				Format:         flagFormat,
				Output:         flagOutput,
				FailOn:         flagFailOn,
				IgnoreDefaults: flagIgnoreDefaults,
				Debug:          flagDebug,
			}
			if strings.TrimSpace(flagResourceTypes) != "" {
				cliOpts.ResourceTypes = config.ParseResourceTypesArg(flagResourceTypes)
			}

			fileCfg, err := config.LoadFile(resolvedConfigPath)
			if err != nil {
				return ExitError{Code: 1, Err: fmt.Errorf("load config: %w", err)}
			}
			opts, err := config.Merge(defaults, fileCfg, cliOpts)
			if err != nil {
				return ExitError{Code: 1, Err: err}
			}
			if err := validateResourceTypes(opts.ResourceTypes); err != nil {
				return ExitError{Code: 1, Err: err}
			}

			svc := scan.NewService(deps.Observer, deps.Version)
			reportData, err := svc.Run(context.Background(), opts)
			if err != nil {
				return ExitError{Code: 1, Err: err}
			}

			if opts.Debug {
				printDebugCounts(deps.Stderr, reportData)
			}

			if err := validateEmptyResult(opts, reportData); err != nil {
				return ExitError{Code: 1, Err: err}
			}

			if err := cache.Save(reportData, cache.DefaultCachePath); err != nil {
				return ExitError{Code: 1, Err: fmt.Errorf("write scan cache: %w", err)}
			}

			if err := writeScanOutput(deps.Stdout, opts.Output, opts.Format, reportData); err != nil {
				return ExitError{Code: 1, Err: err}
			}

			if scan.ShouldFail(reportData.Findings, opts.FailOn) {
				return ExitError{Code: 2, Err: fmt.Errorf("drift detected above threshold: %s", opts.FailOn)}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagPath, "path", ".", "Path to Terraform configuration")
	cmd.Flags().StringVarP(&flagConfig, "config", "f", "", "Path to Terradrift config file (auto-detected when omitted)")
	cmd.Flags().StringVar(&flagProject, "project", "", "GCP project ID")
	cmd.Flags().StringVar(&flagFormat, "format", "text", "Output format: text|json")
	cmd.Flags().StringVar(&flagOutput, "output", "", "Write output to file path")
	cmd.Flags().StringVar(&flagFailOn, "fail-on", "never", "Fail threshold: high|medium|any|never")
	cmd.Flags().StringVar(&flagResourceTypes, "resource-types", "", "Comma-separated resource types")
	cmd.Flags().BoolVar(&flagIgnoreDefaults, "ignore-defaults", false, "Skip known default GCP resources during observed-state scanning")
	cmd.Flags().BoolVar(&flagDebug, "debug", false, "Enable debug diagnostics")

	return cmd
}

func newExplainCmd(deps Deps) *cobra.Command {
	var (
		cachePath string
		format    string
	)
	cmd := &cobra.Command{
		Use:   "explain <finding-id>",
		Short: "Explain a finding from the last scan cache",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reportData, err := cache.Load(cachePath)
			if err != nil {
				return ExitError{Code: 1, Err: fmt.Errorf("load cache: %w", err)}
			}
			findingID := args[0]
			if strings.ToLower(strings.TrimSpace(format)) == "json" {
				payload, err := explainJSON(reportData, findingID)
				if err != nil {
					return ExitError{Code: 1, Err: err}
				}
				_, _ = deps.Stdout.Write(payload)
				return nil
			}
			out, err := report.ExplainText(reportData, findingID)
			if err != nil {
				return ExitError{Code: 1, Err: err}
			}
			_, _ = fmt.Fprintln(deps.Stdout, out)
			return nil
		},
	}
	cmd.Flags().StringVar(&cachePath, "cache", cache.DefaultCachePath, "Path to scan cache JSON")
	cmd.Flags().StringVar(&format, "format", "text", "Explain output format: text|json")
	return cmd
}

func newConfigCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Configuration helpers"}
	cmd.AddCommand(newConfigInitCmd(deps))
	return cmd
}

func newConfigInitCmd(deps Deps) *cobra.Command {
	var (
		output string
		force  bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a Terradrift YAML config",
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				output = "terradrift.yaml"
			}
			if !force {
				if _, err := os.Stat(output); err == nil {
					return ExitError{Code: 1, Err: fmt.Errorf("config already exists: %s (use --force to overwrite)", output)}
				}
			}
			if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
				return ExitError{Code: 1, Err: err}
			}
			if err := os.WriteFile(output, []byte(config.ConfigTemplate()), 0o644); err != nil {
				return ExitError{Code: 1, Err: err}
			}
			_, _ = fmt.Fprintf(deps.Stdout, "wrote %s\n", output)
			return nil
		},
	}
	cmd.Flags().StringVar(&output, "output", "terradrift.yaml", "Config output path")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config file")
	return cmd
}

func newVersionCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print Terradrift version",
		Run: func(cmd *cobra.Command, args []string) {
			line := deps.Version
			if deps.Commit != "" {
				line += fmt.Sprintf(" (commit %s)", deps.Commit)
			}
			if deps.Date != "" {
				line += fmt.Sprintf(" built %s", deps.Date)
			}
			_, _ = fmt.Fprintln(deps.Stdout, line)
		},
	}
}

func validateResourceTypes(resourceTypes []string) error {
	allowed := make(map[string]struct{}, len(model.SupportedResourceTypes()))
	for _, rt := range model.SupportedResourceTypes() {
		allowed[rt] = struct{}{}
	}
	for _, rt := range resourceTypes {
		if _, ok := allowed[rt]; !ok {
			return fmt.Errorf("unsupported resource type: %s", rt)
		}
	}
	return nil
}

func printDebugCounts(w io.Writer, report model.ScanReport) {
	expectedCounts := map[string]int{}
	for _, r := range report.ExpectedResources {
		expectedCounts[r.ResourceType]++
	}
	observedCounts := map[string]int{}
	for _, r := range report.ObservedResources {
		observedCounts[r.ResourceType]++
	}

	keys := make([]string, 0, len(expectedCounts)+len(observedCounts))
	seen := map[string]struct{}{}
	for k := range expectedCounts {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range observedCounts {
		if _, ok := seen[k]; ok {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	_, _ = fmt.Fprintln(w, "debug: observed resource counts")
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "debug: type=%s expected=%d observed=%d\n", k, expectedCounts[k], observedCounts[k])
	}
}

func writeScanOutput(stdout io.Writer, outputPath, format string, reportData model.ScanReport) error {
	format = strings.ToLower(strings.TrimSpace(format))
	var payload []byte
	var err error
	if format == "json" {
		payload, err = report.ToJSON(reportData)
		if err != nil {
			return err
		}
	} else {
		payload = []byte(report.ToText(reportData))
		if len(payload) == 0 || payload[len(payload)-1] != '\n' {
			payload = append(payload, '\n')
		}
	}

	if strings.TrimSpace(outputPath) == "" {
		_, err = stdout.Write(payload)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, payload, 0o644)
}

func explainJSON(reportData model.ScanReport, findingID string) ([]byte, error) {
	var f *model.Finding
	for i := range reportData.Findings {
		if reportData.Findings[i].ID == findingID {
			f = &reportData.Findings[i]
			break
		}
	}
	if f == nil {
		return nil, fmt.Errorf("finding not found: %s", findingID)
	}

	var expected *model.ExpectedResource
	if f.ExpectedAddress != "" {
		for i := range reportData.ExpectedResources {
			if reportData.ExpectedResources[i].Address == f.ExpectedAddress {
				expected = &reportData.ExpectedResources[i]
				break
			}
		}
	}
	var observed *model.ObservedResource
	if f.ObservedProvider != "" {
		for i := range reportData.ObservedResources {
			if reportData.ObservedResources[i].ProviderID == f.ObservedProvider {
				observed = &reportData.ObservedResources[i]
				break
			}
		}
	}

	payload := map[string]any{
		"finding":  f,
		"intent":   expected,
		"observed": observed,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func validateEmptyResult(opts config.ScanOptions, reportData model.ScanReport) error {
	if len(reportData.ExpectedResources) != 0 || len(reportData.ObservedResources) != 0 {
		return nil
	}

	project := opts.Project
	if strings.TrimSpace(project) == "" {
		project = "<unset>"
	}
	return fmt.Errorf(
		"scan returned no resources for types [%s] in project %q. API enumeration succeeded, so either these resources do not exist or the project mapping is incorrect in config/env",
		strings.Join(opts.ResourceTypes, ", "),
		project,
	)
}
