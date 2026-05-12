package scan

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	intentterraform "terradrift/src/intent/terraform"
	"terradrift/src/internal/config"
	"terradrift/src/internal/model"
)

type Observer interface {
	Observe(ctx context.Context, project string, resourceTypes []string) ([]model.ObservedResource, error)
}

type IntentParser func(path string, project string, resourceTypes []string) ([]model.ExpectedResource, error)

type Service struct {
	Observer    Observer
	ParseIntent IntentParser
	Version     string
}

func NewService(observer Observer, version string) *Service {
	return &Service{
		Observer:    observer,
		ParseIntent: intentterraform.ParseExpected,
		Version:     version,
	}
}

func (s *Service) Run(ctx context.Context, opts config.ScanOptions) (model.ScanReport, error) {
	if s.Observer == nil {
		return model.ScanReport{}, fmt.Errorf("observer is required")
	}
	if s.ParseIntent == nil {
		return model.ScanReport{}, fmt.Errorf("intent parser is required")
	}

	expected, err := s.ParseIntent(opts.Path, opts.Project, opts.ResourceTypes)
	if err != nil {
		return model.ScanReport{}, err
	}
	expected = filterExpectedResourcesByType(expected, opts.ResourceTypes)
	observed, err := s.Observer.Observe(ctx, opts.Project, opts.ResourceTypes)
	if err != nil {
		return model.ScanReport{}, err
	}
	observed = filterObservedResourcesByType(observed, opts.ResourceTypes)
	if opts.IgnoreDefaults {
		observed = filterDefaultObservedResources(observed)
	}

	matches, findings := computeFindings(expected, observed)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].ExpectedAddress == matches[j].ExpectedAddress {
			return matches[i].ObservedProvider < matches[j].ObservedProvider
		}
		return matches[i].ExpectedAddress < matches[j].ExpectedAddress
	})
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].ID < findings[j].ID
	})

	report := model.ScanReport{
		SchemaVersion: 1,
		Metadata: model.Metadata{
			Provider:       "gcp",
			Project:        opts.Project,
			Path:           opts.Path,
			ResourceTypes:  append([]string{}, opts.ResourceTypes...),
			IgnoreDefaults: opts.IgnoreDefaults,
			Version:        s.Version,
		},
		ExpectedResources: expected,
		ObservedResources: observed,
		Matches:           matches,
		Findings:          findings,
	}
	return report, nil
}

func computeFindings(expected []model.ExpectedResource, observed []model.ObservedResource) ([]model.Match, []model.Finding) {
	obsByKey := map[string][]int{}
	for i, o := range observed {
		key := observationKey(o.ResourceType, o.Identity)
		obsByKey[key] = append(obsByKey[key], i)
	}

	usedObserved := map[int]bool{}
	matches := make([]model.Match, 0)
	findings := make([]model.Finding, 0)

	for _, e := range expected {
		if !e.IdentityKnown || len(e.Identity) == 0 {
			f := model.Finding{
				ResourceType:    e.ResourceType,
				FindingType:     model.FindingTypeUnknownIntent,
				ExpectedAddress: e.Address,
				Notes:           []string{"identity cannot be resolved from literal Terraform values"},
				Evidence: map[string]any{
					"classification_rule": "unresolved_identity",
				},
			}
			classifyFinding(&f)
			f.ID = model.StableFindingID(f)
			findings = append(findings, f)
			continue
		}

		key := observationKey(e.ResourceType, e.Identity)
		candidates := obsByKey[key]
		matchedIndex := -1
		for _, idx := range candidates {
			if !usedObserved[idx] {
				matchedIndex = idx
				break
			}
		}
		if matchedIndex == -1 {
			f := model.Finding{
				ResourceType:    e.ResourceType,
				FindingType:     model.FindingTypeMissing,
				ExpectedAddress: e.Address,
				Notes:           []string{"resource declared in Terraform was not found in GCP"},
				Evidence: map[string]any{
					"classification_rule": "expected_without_observed_match",
				},
			}
			classifyFinding(&f)
			f.ID = model.StableFindingID(f)
			findings = append(findings, f)
			continue
		}

		usedObserved[matchedIndex] = true
		o := observed[matchedIndex]
		matches = append(matches, model.Match{
			ExpectedAddress:  e.Address,
			ObservedProvider: o.ProviderID,
			Confidence:       "HIGH",
			Reason:           "canonical identity match",
		})

		diffs := diffExpectedObserved(e, o)
		if len(diffs) == 0 {
			continue
		}
		f := model.Finding{
			ResourceType:     e.ResourceType,
			FindingType:      model.FindingTypeMismatch,
			ExpectedAddress:  e.Address,
			ObservedProvider: o.ProviderID,
			Diff:             diffs,
		}
		classifyFinding(&f)
		f.ID = model.StableFindingID(f)
		findings = append(findings, f)
	}

	for i, o := range observed {
		if usedObserved[i] {
			continue
		}
		f := model.Finding{
			ResourceType:     o.ResourceType,
			FindingType:      model.FindingTypeExtra,
			ObservedProvider: o.ProviderID,
			Notes:            []string{"resource exists in GCP but is not represented in parsed Terraform intent"},
			Evidence: map[string]any{
				"classification_rule": "observed_without_expected_match",
			},
		}
		classifyFinding(&f)
		f.ID = model.StableFindingID(f)
		findings = append(findings, f)
	}

	return matches, findings
}

func filterExpectedResourcesByType(resources []model.ExpectedResource, resourceTypes []string) []model.ExpectedResource {
	if len(resourceTypes) == 0 {
		return resources
	}
	allowed := resourceTypeSet(resourceTypes)
	filtered := make([]model.ExpectedResource, 0, len(resources))
	for _, resource := range resources {
		if _, ok := allowed[resource.ResourceType]; ok {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func filterObservedResourcesByType(resources []model.ObservedResource, resourceTypes []string) []model.ObservedResource {
	if len(resourceTypes) == 0 {
		return resources
	}
	allowed := resourceTypeSet(resourceTypes)
	filtered := make([]model.ObservedResource, 0, len(resources))
	for _, resource := range resources {
		if _, ok := allowed[resource.ResourceType]; ok {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func resourceTypeSet(resourceTypes []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(resourceTypes))
	for _, resourceType := range resourceTypes {
		allowed[resourceType] = struct{}{}
	}
	return allowed
}

func filterDefaultObservedResources(observed []model.ObservedResource) []model.ObservedResource {
	filtered := make([]model.ObservedResource, 0, len(observed))
	for _, resource := range observed {
		if isDefaultObservedResource(resource) {
			continue
		}
		filtered = append(filtered, resource)
	}
	return filtered
}

func isDefaultObservedResource(resource model.ObservedResource) bool {
	switch resource.ResourceType {
	case model.ResourceTypeComputeNetwork:
		return identityString(resource.Identity, "name") == "default" ||
			strings.HasSuffix(resource.ProviderID, "/global/networks/default")
	case model.ResourceTypeComputeSubnetwork:
		return identityString(resource.Identity, "name") == "default" && identityString(resource.Normalized, "network") == "default" ||
			strings.HasSuffix(resource.ProviderID, "/subnetworks/default")
	case model.ResourceTypeServiceAccount:
		email := identityString(resource.Identity, "email")
		if email == "" {
			email = identityString(resource.Normalized, "email")
		}
		if email == "" {
			accountID := identityString(resource.Normalized, "account_id")
			project := identityString(resource.Normalized, "project")
			if accountID != "" && project != "" {
				email = accountID + "@" + project + ".iam.gserviceaccount.com"
			}
		}
		return isDefaultServiceAccountEmail(email)
	default:
		return false
	}
}

func identityString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return value
}

func isDefaultServiceAccountEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	return strings.HasSuffix(email, "-compute@developer.gserviceaccount.com") ||
		strings.HasSuffix(email, "@appspot.gserviceaccount.com") ||
		strings.HasSuffix(email, "@cloudbuild.gserviceaccount.com")
}

func observationKey(resourceType string, identity map[string]any) string {
	return resourceType + "|" + model.CanonicalIdentity(identity)
}

func diffExpectedObserved(expected model.ExpectedResource, observed model.ObservedResource) []model.DiffEntry {
	keys := make([]string, 0, len(expected.Normalized))
	for field := range expected.Normalized {
		keys = append(keys, field)
	}
	sort.Strings(keys)

	diffs := make([]model.DiffEntry, 0)
	for _, field := range keys {
		eVal := expected.Normalized[field]
		oVal, ok := observed.Normalized[field]
		if !ok {
			if field == "labels" && stringMapLen(eVal) == 0 {
				continue
			}
			diffs = append(diffs, model.DiffEntry{
				Field:            field,
				Expected:         eVal,
				Observed:         nil,
				ExpectedExplicit: expected.FieldExplicit[field],
			})
			continue
		}
		if field == "labels" {
			if match, comparable := labelMapMatches(eVal, oVal); comparable {
				if !match {
					diffs = append(diffs, model.DiffEntry{
						Field:            field,
						Expected:         eVal,
						Observed:         oVal,
						ExpectedExplicit: expected.FieldExplicit[field],
					})
				}
				continue
			}
		}
		if !reflect.DeepEqual(eVal, oVal) {
			diffs = append(diffs, model.DiffEntry{
				Field:            field,
				Expected:         eVal,
				Observed:         oVal,
				ExpectedExplicit: expected.FieldExplicit[field],
			})
		}
	}
	return diffs
}

func labelMapMatches(expected any, observed any) (bool, bool) {
	expectedLabels, ok := anyStringMap(expected)
	if !ok {
		return false, false
	}
	observedLabels, ok := anyStringMap(observed)
	if !ok {
		return false, false
	}
	for key, expectedValue := range expectedLabels {
		if observedValue, ok := observedLabels[key]; !ok || observedValue != expectedValue {
			return false, true
		}
	}
	return true, true
}

func stringMapLen(value any) int {
	labels, ok := anyStringMap(value)
	if !ok {
		return -1
	}
	return len(labels)
}

func anyStringMap(value any) (map[string]string, bool) {
	switch labels := value.(type) {
	case map[string]string:
		return labels, true
	case map[string]any:
		out := make(map[string]string, len(labels))
		for key, value := range labels {
			strValue, ok := value.(string)
			if !ok {
				return nil, false
			}
			out[key] = strValue
		}
		return out, true
	default:
		return nil, false
	}
}

func classifyFinding(f *model.Finding) {
	switch f.FindingType {
	case model.FindingTypeExtra:
		f.Cause = model.CauseLegacyArtifact
		f.Severity = model.SeverityMedium
		if f.Evidence == nil {
			f.Evidence = map[string]any{}
		}
		appendRule(f, "extra_resource")
		return
	case model.FindingTypeUnknownIntent:
		f.Cause = model.CauseTerraformComp
		f.Severity = model.SeverityMedium
		if f.Evidence == nil {
			f.Evidence = map[string]any{}
		}
		appendRule(f, "unknown_intent")
		return
	case model.FindingTypeMissing:
		f.Cause = model.CauseUnknown
		f.Severity = model.SeverityHigh
		if f.Evidence == nil {
			f.Evidence = map[string]any{}
		}
		appendRule(f, "missing_resource")
		return
	case model.FindingTypeMismatch:
		if labelOnlyDiff(f.Diff) {
			f.Cause = model.CauseManualChange
			f.Severity = model.SeverityMedium
			f.Notes = append(f.Notes, "only labels differ")
			appendRule(f, "label_only")
			return
		}
		if defaultOnlyDiff(f.Diff) {
			f.Cause = model.CausePlatformDefault
			f.Severity = model.SeverityLow
			f.Notes = append(f.Notes, "differences are on implicit default fields")
			appendRule(f, "default_only")
			return
		}
		f.Cause = model.CauseUnknown
		f.Severity = model.SeverityHigh
		appendRule(f, "fallback_unknown")
		return
	default:
		f.Cause = model.CauseUnknown
		f.Severity = model.SeverityHigh
		appendRule(f, "fallback_unknown")
	}
}

func appendRule(f *model.Finding, rule string) {
	evidenceMap, ok := f.Evidence.(map[string]any)
	if !ok {
		evidenceMap = map[string]any{}
	}
	evidenceMap["classification_rule"] = rule
	f.Evidence = evidenceMap
}

func labelOnlyDiff(diffs []model.DiffEntry) bool {
	if len(diffs) == 0 {
		return false
	}
	for _, d := range diffs {
		if d.Field != "labels" && !strings.HasPrefix(d.Field, "labels.") {
			return false
		}
	}
	return true
}

func defaultOnlyDiff(diffs []model.DiffEntry) bool {
	if len(diffs) == 0 {
		return false
	}
	for _, d := range diffs {
		if d.ExpectedExplicit {
			return false
		}
	}
	return true
}

func ShouldFail(findings []model.Finding, failOn string) bool {
	mode := strings.ToLower(strings.TrimSpace(failOn))
	switch mode {
	case "never":
		return false
	case "any":
		return len(findings) > 0
	case "medium":
		for _, f := range findings {
			if f.Severity == model.SeverityHigh || f.Severity == model.SeverityMedium {
				return true
			}
		}
		return false
	case "high":
		for _, f := range findings {
			if f.Severity == model.SeverityHigh {
				return true
			}
		}
		return false
	default:
		return false
	}
}
