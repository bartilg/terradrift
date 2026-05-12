# Terradrift

Terradrift is a CLI tool for detecting infrastructure drift by comparing Terraform configuration (*.tf) with live cloud resources without relying on pre-existing tfstate. It aims to surface:

- Missing resources (declared but absent)
- Unmanaged resources (present but undeclared)
- Changed resources (attribute mismatches)

## Status

Current GCP support:
- Terraform intent parsing for Cloud Run services, Cloud Storage buckets, service accounts, Compute Engine networks/subnetworks/instances, Pub/Sub topics, BigQuery datasets, Artifact Registry repositories, and Secret Manager secrets
- Live GCP enumeration for the same resource types
- Drift classification for missing, unmanaged, unknown-intent, and mismatched resources
- CLI subcommands: `scan`, `explain`, `config init`, and `version`

## Usage

For full setup instructions, see [INSTALL.md](INSTALL.md).

Build:
``` 
go build ./src/cmd/terradrift
```

Run without installing:
``` 
go run ./src/cmd/terradrift --help
```

Scan the current directory:
```
terradrift scan --path . --project <gcp-project-id>
```

JSON output:
```
terradrift scan --path . --project <gcp-project-id> --format json
```

Scan the included sample project:
```
terradrift scan --config terradrift.yaml --project <gcp-project-id>
```

## Roadmap (from project plan)
- Attribute normalization & richer diff strategy
- GitHub Actions integration & JSON artifact export
- Additional cloud/provider coverage
- Performance benchmarking & documentation

## License
MIT (to be completed in LICENSE file)

## Repository layout

Go source now lives under `src/`:

- `src/cmd/terradrift` for the CLI entrypoint
- `src/internal` for application internals
- `src/intent` for Terraform intent parsing
- `src/providers` for cloud provider integrations
