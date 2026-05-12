package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"terradrift/src/internal/model"
)

func ToJSON(report model.ScanReport) ([]byte, error) {
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func ToText(report model.ScanReport) string {
	var b strings.Builder

	high, medium, low := countBySeverity(report.Findings)
	fmt.Fprintf(&b, "Summary\n")
	fmt.Fprintf(&b, "  Expected: %d\n", len(report.ExpectedResources))
	fmt.Fprintf(&b, "  Observed: %d\n", len(report.ObservedResources))
	fmt.Fprintf(&b, "  Matches:  %d\n", len(report.Matches))
	fmt.Fprintf(&b, "  Findings: %d (HIGH=%d MEDIUM=%d LOW=%d)\n\n", len(report.Findings), high, medium, low)

	if len(report.Findings) == 0 {
		b.WriteString("Findings\n  none\n")
		return b.String()
	}

	b.WriteString("Findings\n")
	tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTYPE\tRESOURCE\tSEVERITY\tCAUSE\tSUMMARY")
	for _, f := range report.Findings {
		resource := summarizeResource(f)
		summary := summarizeFinding(f)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", f.ID, f.FindingType, resource, f.Severity, f.Cause, summary)
	}
	_ = tw.Flush()
	return b.String()
}

func ExplainText(report model.ScanReport, findingID string) (string, error) {
	var finding *model.Finding
	for i := range report.Findings {
		if report.Findings[i].ID == findingID {
			finding = &report.Findings[i]
			break
		}
	}
	if finding == nil {
		return "", fmt.Errorf("finding not found: %s", findingID)
	}

	intent := findExpected(report.ExpectedResources, finding.ExpectedAddress)
	observed := findObserved(report.ObservedResources, finding.ObservedProvider)

	var b strings.Builder
	fmt.Fprintf(&b, "Finding %s\n", finding.ID)
	fmt.Fprintf(&b, "  Type: %s\n", finding.FindingType)
	fmt.Fprintf(&b, "  Resource Type: %s\n", finding.ResourceType)
	fmt.Fprintf(&b, "  Severity: %s\n", finding.Severity)
	fmt.Fprintf(&b, "  Cause: %s\n", finding.Cause)
	if finding.ExpectedAddress != "" {
		fmt.Fprintf(&b, "  Expected Address: %s\n", finding.ExpectedAddress)
	}
	if finding.ObservedProvider != "" {
		fmt.Fprintf(&b, "  Observed Provider ID: %s\n", finding.ObservedProvider)
	}

	if len(finding.Notes) > 0 {
		fmt.Fprintf(&b, "\nNotes\n")
		for _, n := range finding.Notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}

	if evidence, ok := finding.Evidence.(map[string]any); ok && len(evidence) > 0 {
		fmt.Fprintf(&b, "\nClassification\n")
		keys := make([]string, 0, len(evidence))
		for k := range evidence {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %s: %v\n", k, evidence[k])
		}
	}

	if intent != nil {
		fmt.Fprintf(&b, "\nIntent\n%s\n", indentedJSON(*intent))
	}
	if observed != nil {
		fmt.Fprintf(&b, "\nObserved\n%s\n", indentedJSON(*observed))
	}

	if len(finding.Diff) > 0 {
		fmt.Fprintf(&b, "\nDiff\n")
		for _, d := range finding.Diff {
			fmt.Fprintf(&b, "  - %s: expected=%v observed=%v\n", d.Field, d.Expected, d.Observed)
		}
	}

	return b.String(), nil
}

func summarizeResource(f model.Finding) string {
	if f.ExpectedAddress != "" && f.ObservedProvider != "" {
		return f.ExpectedAddress
	}
	if f.ExpectedAddress != "" {
		return f.ExpectedAddress
	}
	if f.ObservedProvider != "" {
		return f.ObservedProvider
	}
	return f.ResourceType
}

func summarizeFinding(f model.Finding) string {
	if len(f.Notes) > 0 {
		return f.Notes[0]
	}
	if len(f.Diff) > 0 {
		fields := make([]string, 0, len(f.Diff))
		for _, d := range f.Diff {
			fields = append(fields, d.Field)
		}
		sort.Strings(fields)
		if len(fields) > 3 {
			fields = fields[:3]
		}
		return "mismatch fields: " + strings.Join(fields, ",")
	}
	switch f.FindingType {
	case model.FindingTypeMissing:
		return "expected resource missing in observed state"
	case model.FindingTypeExtra:
		return "observed resource missing from intent"
	case model.FindingTypeUnknownIntent:
		return "intent uses computed or unknown identity"
	default:
		return "drift detected"
	}
}

func countBySeverity(findings []model.Finding) (high, medium, low int) {
	for _, f := range findings {
		switch f.Severity {
		case model.SeverityHigh:
			high++
		case model.SeverityMedium:
			medium++
		case model.SeverityLow:
			low++
		}
	}
	return
}

func findExpected(resources []model.ExpectedResource, address string) *model.ExpectedResource {
	if address == "" {
		return nil
	}
	for i := range resources {
		if resources[i].Address == address {
			return &resources[i]
		}
	}
	return nil
}

func findObserved(resources []model.ObservedResource, providerID string) *model.ObservedResource {
	if providerID == "" {
		return nil
	}
	for i := range resources {
		if resources[i].ProviderID == providerID {
			return &resources[i]
		}
	}
	return nil
}

func indentedJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "  <unavailable>"
	}
	buf := bytes.NewBuffer(nil)
	for _, line := range strings.Split(string(b), "\n") {
		buf.WriteString("  ")
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
