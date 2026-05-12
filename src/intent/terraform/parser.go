package terraform

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"terradrift/src/internal/model"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

type parsedFile struct {
	path string
	body *hclsyntax.Body
}

func ParseExpected(root string, project string, resourceTypes []string) ([]model.ExpectedResource, error) {
	allowed := map[string]struct{}{}
	for _, rt := range resourceTypes {
		allowed[rt] = struct{}{}
	}

	files, err := collectTF(root)
	if err != nil {
		return nil, err
	}

	parser := hclparse.NewParser()
	parsed := make([]parsedFile, 0, len(files))
	for _, path := range files {
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			return nil, fmt.Errorf("parse %s: %s", path, diags.Error())
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		parsed = append(parsed, parsedFile{path: path, body: body})
	}

	evalCtx := buildEvalContext(root, parsed)
	providerProject := resolveGoogleProviderProject(parsed, evalCtx, project)

	expected := make([]model.ExpectedResource, 0)
	for _, file := range parsed {
		for _, block := range file.body.Blocks {
			if block.Type != "resource" || len(block.Labels) < 2 {
				continue
			}
			resourceType := block.Labels[0]
			if _, ok := allowed[resourceType]; !ok {
				continue
			}

			var r model.ExpectedResource
			switch resourceType {
			case model.ResourceTypeArtifactRegistryRepository:
				r = parseArtifactRegistryRepository(block, file.path, evalCtx)
			case model.ResourceTypeBigQueryDataset:
				r = parseBigQueryDataset(block, file.path, evalCtx)
			case model.ResourceTypeBucket:
				r = parseBucket(block, file.path, evalCtx)
			case model.ResourceTypeServiceAccount:
				r = parseServiceAccount(block, providerProject, file.path, evalCtx)
			case model.ResourceTypeCloudRunService:
				r = parseCloudRunService(block, file.path, evalCtx)
			case model.ResourceTypeComputeInstance:
				r = parseComputeInstance(block, file.path, evalCtx)
			case model.ResourceTypeComputeNetwork:
				r = parseComputeNetwork(block, file.path, evalCtx)
			case model.ResourceTypeComputeSubnetwork:
				r = parseComputeSubnetwork(block, file.path, evalCtx)
			case model.ResourceTypePubSubTopic:
				r = parsePubSubTopic(block, file.path, evalCtx)
			case model.ResourceTypeSecretManagerSecret:
				r = parseSecretManagerSecret(block, file.path, evalCtx)
			default:
				continue
			}
			expected = append(expected, r)
		}
	}

	sort.Slice(expected, func(i, j int) bool {
		if expected[i].Address == expected[j].Address {
			return expected[i].ResourceType < expected[j].ResourceType
		}
		return expected[i].Address < expected[j].Address
	})

	return expected, nil
}

func collectTF(root string) ([]string, error) {
	out := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".terraform" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".tf" {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func buildEvalContext(root string, files []parsedFile) *hcl.EvalContext {
	vars := map[string]cty.Value{}
	locals := map[string]cty.Value{}
	for _, file := range files {
		for _, block := range file.body.Blocks {
			if block.Type != "variable" || len(block.Labels) != 1 {
				continue
			}
			if v, present, known := literalAttr(block.Body, "default"); present && known {
				vars[block.Labels[0]] = v
			}
		}
	}

	for _, file := range files {
		for _, block := range file.body.Blocks {
			if block.Type != "locals" {
				continue
			}
			for name, attr := range block.Body.Attributes {
				value, diags := attr.Expr.Value(evalContextFromValues(vars, locals))
				if diags.HasErrors() || !value.IsWhollyKnown() {
					continue
				}
				locals[name] = value
			}
		}
	}

	tfvars := collectTFVars(root)
	parser := hclparse.NewParser()
	for _, path := range tfvars {
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			continue
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		for name, attr := range body.Attributes {
			value, diags := attr.Expr.Value(evalContextFromValues(vars, locals))
			if diags.HasErrors() || !value.IsWhollyKnown() {
				continue
			}
			vars[name] = value
		}
	}

	locals = map[string]cty.Value{}
	for _, file := range files {
		for _, block := range file.body.Blocks {
			if block.Type != "locals" {
				continue
			}
			for name, attr := range block.Body.Attributes {
				value, diags := attr.Expr.Value(evalContextFromValues(vars, locals))
				if diags.HasErrors() || !value.IsWhollyKnown() {
					continue
				}
				locals[name] = value
			}
		}
	}

	return evalContextFromValues(vars, locals)
}

func collectTFVars(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "terraform.tfvars" || strings.HasSuffix(name, ".auto.tfvars") {
			out = append(out, filepath.Join(root, name))
		}
	}
	sort.Strings(out)
	return out
}

func evalContextFromValues(vars map[string]cty.Value, locals map[string]cty.Value) *hcl.EvalContext {
	if len(vars) == 0 && len(locals) == 0 {
		return &hcl.EvalContext{Functions: terraformFunctions()}
	}
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{},
		Functions: terraformFunctions(),
	}
	if len(vars) > 0 {
		ctx.Variables["var"] = cty.ObjectVal(vars)
	}
	if len(locals) > 0 {
		ctx.Variables["local"] = cty.ObjectVal(locals)
	}
	return ctx
}

func terraformFunctions() map[string]function.Function {
	return map[string]function.Function{
		"abs":                    stdlib.AbsoluteFunc,
		"ceil":                   stdlib.CeilFunc,
		"can":                    tryfunc.CanFunc,
		"chomp":                  stdlib.ChompFunc,
		"chunklist":              stdlib.ChunklistFunc,
		"coalesce":               stdlib.CoalesceFunc,
		"coalescelist":           stdlib.CoalesceListFunc,
		"compact":                stdlib.CompactFunc,
		"concat":                 stdlib.ConcatFunc,
		"contains":               stdlib.ContainsFunc,
		"csvdecode":              stdlib.CSVDecodeFunc,
		"distinct":               stdlib.DistinctFunc,
		"element":                stdlib.ElementFunc,
		"flatten":                stdlib.FlattenFunc,
		"floor":                  stdlib.FloorFunc,
		"format":                 stdlib.FormatFunc,
		"formatdate":             stdlib.FormatDateFunc,
		"formatlist":             stdlib.FormatListFunc,
		"indent":                 stdlib.IndentFunc,
		"index":                  stdlib.IndexFunc,
		"join":                   stdlib.JoinFunc,
		"jsondecode":             stdlib.JSONDecodeFunc,
		"jsonencode":             stdlib.JSONEncodeFunc,
		"keys":                   stdlib.KeysFunc,
		"length":                 stdlib.LengthFunc,
		"log":                    stdlib.LogFunc,
		"lookup":                 stdlib.LookupFunc,
		"lower":                  stdlib.LowerFunc,
		"max":                    stdlib.MaxFunc,
		"merge":                  stdlib.MergeFunc,
		"min":                    stdlib.MinFunc,
		"parseint":               stdlib.ParseIntFunc,
		"pow":                    stdlib.PowFunc,
		"range":                  stdlib.RangeFunc,
		"regex":                  stdlib.RegexFunc,
		"regexall":               stdlib.RegexAllFunc,
		"replace":                stdlib.ReplaceFunc,
		"reverse":                stdlib.ReverseFunc,
		"setproduct":             stdlib.SetProductFunc,
		"setintersection":        stdlib.SetIntersectionFunc,
		"setsubtract":            stdlib.SetSubtractFunc,
		"setsymmetricdifference": stdlib.SetSymmetricDifferenceFunc,
		"setunion":               stdlib.SetUnionFunc,
		"signum":                 stdlib.SignumFunc,
		"slice":                  stdlib.SliceFunc,
		"sort":                   stdlib.SortFunc,
		"split":                  stdlib.SplitFunc,
		"strrev":                 stdlib.ReverseFunc,
		"substr":                 stdlib.SubstrFunc,
		"timeadd":                stdlib.TimeAddFunc,
		"title":                  stdlib.TitleFunc,
		"tobool":                 stdlib.MakeToFunc(cty.Bool),
		"tolist":                 stdlib.MakeToFunc(cty.List(cty.DynamicPseudoType)),
		"tomap":                  stdlib.MakeToFunc(cty.Map(cty.DynamicPseudoType)),
		"tonumber":               stdlib.MakeToFunc(cty.Number),
		"toset":                  stdlib.MakeToFunc(cty.Set(cty.DynamicPseudoType)),
		"tostring":               stdlib.MakeToFunc(cty.String),
		"trim":                   stdlib.TrimFunc,
		"trimprefix":             stdlib.TrimPrefixFunc,
		"trimspace":              stdlib.TrimSpaceFunc,
		"trimsuffix":             stdlib.TrimSuffixFunc,
		"try":                    tryfunc.TryFunc,
		"upper":                  stdlib.UpperFunc,
		"values":                 stdlib.ValuesFunc,
		"zipmap":                 stdlib.ZipmapFunc,
	}
}

func resolveGoogleProviderProject(files []parsedFile, evalCtx *hcl.EvalContext, fallback string) string {
	for _, file := range files {
		for _, block := range file.body.Blocks {
			if block.Type != "provider" || len(block.Labels) == 0 || block.Labels[0] != "google" {
				continue
			}
			if v, present, known := literalAttrWithContext(block.Body, "project", evalCtx); present && known && v.Type() == cty.String {
				return v.AsString()
			}
		}
	}
	return fallback
}

func parseBucket(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	if v, present, known := literalAttrWithContext(block.Body, "name", evalCtx); present && known && v.Type() == cty.String {
		nameValue := v.AsString()
		r.Identity = map[string]any{"name": nameValue}
		r.IdentityKnown = true
		r.Normalized["name"] = nameValue
		r.FieldExplicit["name"] = true
	} else if !present {
		// Name is required by Terraform schema, but we remain conservative in parsing.
		r.IdentityKnown = false
	}

	if v, present, known := literalAttrWithContext(block.Body, "location", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["location"] = v.AsString()
		r.FieldExplicit["location"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "storage_class", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["storage_class"] = v.AsString()
		r.FieldExplicit["storage_class"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "uniform_bucket_level_access", evalCtx); present && known && v.Type() == cty.Bool {
		r.Normalized["uniform_bucket_level_access"] = v.True()
		r.FieldExplicit["uniform_bucket_level_access"] = true
	} else if !present {
		r.Normalized["uniform_bucket_level_access"] = false
		r.FieldExplicit["uniform_bucket_level_access"] = false
	}
	if v, present, known := literalAttrWithContext(block.Body, "labels", evalCtx); present && known {
		if labels, ok := stringMap(v); ok {
			r.Normalized["labels"] = labels
			r.FieldExplicit["labels"] = true
		}
	} else if !present {
		r.Normalized["labels"] = map[string]string{}
		r.FieldExplicit["labels"] = false
	}

	versioningEnabledSet := false
	for _, nested := range block.Body.Blocks {
		if nested.Type != "versioning" {
			continue
		}
		if v, present, known := literalAttrWithContext(nested.Body, "enabled", evalCtx); present && known && v.Type() == cty.Bool {
			r.Normalized["versioning_enabled"] = v.True()
			r.FieldExplicit["versioning_enabled"] = true
			versioningEnabledSet = true
			break
		}
	}
	if !versioningEnabledSet {
		r.Normalized["versioning_enabled"] = false
		r.FieldExplicit["versioning_enabled"] = false
	}

	return r
}

func parseServiceAccount(block *hclsyntax.Block, project string, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	accountID, accountIDKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "account_id", evalCtx); present && known && v.Type() == cty.String {
		accountID = v.AsString()
		accountIDKnown = true
		r.Normalized["account_id"] = accountID
		r.FieldExplicit["account_id"] = true
	}

	resourceProject := project
	if v, present, known := literalAttrWithContext(block.Body, "project", evalCtx); present && known && v.Type() == cty.String {
		resourceProject = v.AsString()
		r.Normalized["project"] = resourceProject
		r.FieldExplicit["project"] = true
	}

	if accountIDKnown && resourceProject != "" {
		r.Identity = map[string]any{"email": fmt.Sprintf("%s@%s.iam.gserviceaccount.com", accountID, resourceProject)}
		r.IdentityKnown = true
	}

	if v, present, known := literalAttrWithContext(block.Body, "display_name", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["display_name"] = v.AsString()
		r.FieldExplicit["display_name"] = true
	} else if !present {
		r.Normalized["display_name"] = ""
		r.FieldExplicit["display_name"] = false
	}
	if v, present, known := literalAttrWithContext(block.Body, "description", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["description"] = v.AsString()
		r.FieldExplicit["description"] = true
	} else if !present {
		r.Normalized["description"] = ""
		r.FieldExplicit["description"] = false
	}
	if v, present, known := literalAttrWithContext(block.Body, "disabled", evalCtx); present && known && v.Type() == cty.Bool {
		r.Normalized["disabled"] = v.True()
		r.FieldExplicit["disabled"] = true
	} else if !present {
		r.Normalized["disabled"] = false
		r.FieldExplicit["disabled"] = false
	}

	return r
}

func parseCloudRunService(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	serviceName, serviceNameKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "name", evalCtx); present && known && v.Type() == cty.String {
		serviceName = v.AsString()
		serviceNameKnown = true
		r.Normalized["name"] = serviceName
		r.FieldExplicit["name"] = true
	}

	location, locationKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "location", evalCtx); present && known && v.Type() == cty.String {
		location = v.AsString()
		locationKnown = true
		r.Normalized["location"] = location
		r.FieldExplicit["location"] = true
	}

	if serviceNameKnown && locationKnown {
		r.Identity = map[string]any{
			"name":     serviceName,
			"location": location,
		}
		r.IdentityKnown = true
	}

	if v, present, known := literalAttrWithContext(block.Body, "ingress", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["ingress"] = v.AsString()
		r.FieldExplicit["ingress"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "labels", evalCtx); present && known {
		if labels, ok := stringMap(v); ok {
			r.Normalized["labels"] = labels
			r.FieldExplicit["labels"] = true
		}
	}

	for _, nested := range block.Body.Blocks {
		if nested.Type != "template" {
			continue
		}
		if v, present, known := literalAttrWithContext(nested.Body, "service_account", evalCtx); present && known && v.Type() == cty.String {
			r.Normalized["service_account"] = v.AsString()
			r.FieldExplicit["service_account"] = true
		}
		for _, child := range nested.Body.Blocks {
			if child.Type != "containers" {
				continue
			}
			if v, present, known := literalAttrWithContext(child.Body, "image", evalCtx); present && known && v.Type() == cty.String {
				r.Normalized["container_image"] = v.AsString()
				r.FieldExplicit["container_image"] = true
			}
			break
		}
		break
	}

	return r
}

func parseComputeNetwork(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	if v, present, known := literalAttrWithContext(block.Body, "name", evalCtx); present && known && v.Type() == cty.String {
		nameValue := v.AsString()
		r.Identity = map[string]any{"name": nameValue}
		r.IdentityKnown = true
		r.Normalized["name"] = nameValue
		r.FieldExplicit["name"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "auto_create_subnetworks", evalCtx); present && known && v.Type() == cty.Bool {
		r.Normalized["auto_create_subnetworks"] = v.True()
		r.FieldExplicit["auto_create_subnetworks"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "routing_mode", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["routing_mode"] = v.AsString()
		r.FieldExplicit["routing_mode"] = true
	}

	return r
}

func parseComputeSubnetwork(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	subnetworkName, nameKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "name", evalCtx); present && known && v.Type() == cty.String {
		subnetworkName = v.AsString()
		nameKnown = true
		r.Normalized["name"] = subnetworkName
		r.FieldExplicit["name"] = true
	}

	region, regionKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "region", evalCtx); present && known && v.Type() == cty.String {
		region = v.AsString()
		regionKnown = true
		r.Normalized["region"] = region
		r.FieldExplicit["region"] = true
	}

	if nameKnown && regionKnown {
		r.Identity = map[string]any{
			"name":   subnetworkName,
			"region": region,
		}
		r.IdentityKnown = true
	}

	if v, present, known := literalAttrWithContext(block.Body, "ip_cidr_range", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["ip_cidr_range"] = v.AsString()
		r.FieldExplicit["ip_cidr_range"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "private_ip_google_access", evalCtx); present && known && v.Type() == cty.Bool {
		r.Normalized["private_ip_google_access"] = v.True()
		r.FieldExplicit["private_ip_google_access"] = true
	}

	return r
}

func parseComputeInstance(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	instanceName, nameKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "name", evalCtx); present && known && v.Type() == cty.String {
		instanceName = v.AsString()
		nameKnown = true
		r.Normalized["name"] = instanceName
		r.FieldExplicit["name"] = true
	}

	zone, zoneKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "zone", evalCtx); present && known && v.Type() == cty.String {
		zone = v.AsString()
		zoneKnown = true
		r.Normalized["zone"] = zone
		r.FieldExplicit["zone"] = true
	}

	if nameKnown && zoneKnown {
		r.Identity = map[string]any{
			"name": instanceName,
			"zone": zone,
		}
		r.IdentityKnown = true
	}

	if v, present, known := literalAttrWithContext(block.Body, "machine_type", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["machine_type"] = v.AsString()
		r.FieldExplicit["machine_type"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "labels", evalCtx); present && known {
		if labels, ok := stringMap(v); ok {
			r.Normalized["labels"] = labels
			r.FieldExplicit["labels"] = true
		}
	} else if !present {
		r.Normalized["labels"] = map[string]string{}
		r.FieldExplicit["labels"] = false
	}
	if v, present, known := literalAttrWithContext(block.Body, "tags", evalCtx); present && known {
		if tags, ok := stringList(v); ok {
			r.Normalized["tags"] = tags
			r.FieldExplicit["tags"] = true
		}
	} else if !present {
		r.Normalized["tags"] = []string{}
		r.FieldExplicit["tags"] = false
	}

	return r
}

func parsePubSubTopic(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	if v, present, known := literalAttrWithContext(block.Body, "name", evalCtx); present && known && v.Type() == cty.String {
		nameValue := v.AsString()
		r.Identity = map[string]any{"name": nameValue}
		r.IdentityKnown = true
		r.Normalized["name"] = nameValue
		r.FieldExplicit["name"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "labels", evalCtx); present && known {
		if labels, ok := stringMap(v); ok {
			r.Normalized["labels"] = labels
			r.FieldExplicit["labels"] = true
		}
	} else if !present {
		r.Normalized["labels"] = map[string]string{}
		r.FieldExplicit["labels"] = false
	}

	return r
}

func parseBigQueryDataset(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	if v, present, known := literalAttrWithContext(block.Body, "dataset_id", evalCtx); present && known && v.Type() == cty.String {
		datasetID := v.AsString()
		r.Identity = map[string]any{"dataset_id": datasetID}
		r.IdentityKnown = true
		r.Normalized["dataset_id"] = datasetID
		r.FieldExplicit["dataset_id"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "location", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["location"] = v.AsString()
		r.FieldExplicit["location"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "friendly_name", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["friendly_name"] = v.AsString()
		r.FieldExplicit["friendly_name"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "labels", evalCtx); present && known {
		if labels, ok := stringMap(v); ok {
			r.Normalized["labels"] = labels
			r.FieldExplicit["labels"] = true
		}
	} else if !present {
		r.Normalized["labels"] = map[string]string{}
		r.FieldExplicit["labels"] = false
	}

	return r
}

func parseArtifactRegistryRepository(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	repositoryID, repositoryKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "repository_id", evalCtx); present && known && v.Type() == cty.String {
		repositoryID = v.AsString()
		repositoryKnown = true
		r.Normalized["repository_id"] = repositoryID
		r.FieldExplicit["repository_id"] = true
	}

	location, locationKnown := "", false
	if v, present, known := literalAttrWithContext(block.Body, "location", evalCtx); present && known && v.Type() == cty.String {
		location = v.AsString()
		locationKnown = true
		r.Normalized["location"] = location
		r.FieldExplicit["location"] = true
	}

	if repositoryKnown && locationKnown {
		r.Identity = map[string]any{
			"repository_id": repositoryID,
			"location":      location,
		}
		r.IdentityKnown = true
	}

	if v, present, known := literalAttrWithContext(block.Body, "format", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["format"] = v.AsString()
		r.FieldExplicit["format"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "description", evalCtx); present && known && v.Type() == cty.String {
		r.Normalized["description"] = v.AsString()
		r.FieldExplicit["description"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "labels", evalCtx); present && known {
		if labels, ok := stringMap(v); ok {
			r.Normalized["labels"] = labels
			r.FieldExplicit["labels"] = true
		}
	} else if !present {
		r.Normalized["labels"] = map[string]string{}
		r.FieldExplicit["labels"] = false
	}

	return r
}

func parseSecretManagerSecret(block *hclsyntax.Block, sourcePath string, evalCtx *hcl.EvalContext) model.ExpectedResource {
	resourceType := block.Labels[0]
	name := block.Labels[1]
	address := fmt.Sprintf("%s.%s", resourceType, name)
	r := model.ExpectedResource{
		Address:       address,
		ResourceType:  resourceType,
		Name:          name,
		IdentityKnown: false,
		Normalized:    map[string]any{},
		FieldExplicit: map[string]bool{},
		Raw: map[string]any{
			"file": sourcePath,
		},
	}

	if v, present, known := literalAttrWithContext(block.Body, "secret_id", evalCtx); present && known && v.Type() == cty.String {
		secretID := v.AsString()
		r.Identity = map[string]any{"secret_id": secretID}
		r.IdentityKnown = true
		r.Normalized["secret_id"] = secretID
		r.FieldExplicit["secret_id"] = true
	}
	if v, present, known := literalAttrWithContext(block.Body, "labels", evalCtx); present && known {
		if labels, ok := stringMap(v); ok {
			r.Normalized["labels"] = labels
			r.FieldExplicit["labels"] = true
		}
	} else if !present {
		r.Normalized["labels"] = map[string]string{}
		r.FieldExplicit["labels"] = false
	}

	for _, nested := range block.Body.Blocks {
		if nested.Type != "replication" {
			continue
		}
		for _, child := range nested.Body.Blocks {
			switch child.Type {
			case "auto":
				r.Normalized["replication"] = "automatic"
				r.FieldExplicit["replication"] = true
			case "user_managed":
				r.Normalized["replication"] = "user_managed"
				r.FieldExplicit["replication"] = true
			}
		}
		break
	}

	return r
}

func literalAttr(body *hclsyntax.Body, name string) (cty.Value, bool, bool) {
	return literalAttrWithContext(body, name, &hcl.EvalContext{})
}

func literalAttrWithContext(body *hclsyntax.Body, name string, evalCtx *hcl.EvalContext) (cty.Value, bool, bool) {
	attr, ok := body.Attributes[name]
	if !ok {
		return cty.NilVal, false, false
	}
	if evalCtx == nil {
		evalCtx = &hcl.EvalContext{}
	}
	value, diags := attr.Expr.Value(evalCtx)
	if diags.HasErrors() || !value.IsWhollyKnown() {
		return cty.NilVal, true, false
	}
	return value, true, true
}

func stringMap(v cty.Value) (map[string]string, bool) {
	if !(v.Type().IsObjectType() || v.Type().IsMapType()) {
		return nil, false
	}
	it := v.ElementIterator()
	out := map[string]string{}
	for it.Next() {
		k, value := it.Element()
		if value.Type() != cty.String {
			return nil, false
		}
		out[k.AsString()] = value.AsString()
	}
	return out, true
}

func stringList(v cty.Value) ([]string, bool) {
	if !(v.Type().IsTupleType() || v.Type().IsListType() || v.Type().IsSetType()) {
		return nil, false
	}
	it := v.ElementIterator()
	out := make([]string, 0)
	for it.Next() {
		_, value := it.Element()
		if value.Type() != cty.String {
			return nil, false
		}
		out = append(out, value.AsString())
	}
	sort.Strings(out)
	return out, true
}
