# Terradrift Installation Guide

This guide installs Terradrift from source and configures it to scan Terraform
configuration against live GCP resources.

## Prerequisites

- Go `1.25.6` or newer compatible with the version in `go.mod`
- Git
- Google Cloud CLI (`gcloud`) for authentication and permission setup
- Access to a GCP project with permission to read the resources you want to scan
- Terraform only if you want to provision or modify the included `gcp-test` sample

Terradrift parses Terraform files directly. It does not require existing
Terraform state to run a scan.

## 1. Get The Source

```bash
git clone <repo-url> terradrift
cd terradrift
```

If you already have the repository, update it first:

```bash
git pull
```

## 2. Verify The Checkout

```bash
go test ./...
```

All packages should pass before installing or using the CLI.

## 3. Build The Binary

Build a local binary in the repository:

```bash
go build -o bin/terradrift ./src/cmd/terradrift
```

Run it directly:

```bash
./bin/terradrift --help
```

## 4. Install On Your PATH

Install the CLI into your Go binary directory:

```bash
go install ./src/cmd/terradrift
```

Make sure your Go binary directory is on `PATH`. For most local setups:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Verify the installed command:

```bash
terradrift version
terradrift --help
```

## 5. Authenticate To GCP

Terradrift uses Application Default Credentials.

For local user authentication:

```bash
gcloud auth application-default login
```

For service account authentication, set `GOOGLE_APPLICATION_CREDENTIALS` to an
absolute path:

```bash
export GOOGLE_APPLICATION_CREDENTIALS=/absolute/path/to/key.json
```

## 6. Grant Scan Permissions

Run the helper script as a project admin or owner:

```bash
scripts/assign_scan_permissions.sh <project-id-or-number> <member>
```

Example:

```bash
scripts/assign_scan_permissions.sh my-project user:me@example.com
```

The script enables required APIs and grants read-only roles for discovery.

## 7. Create A Terradrift Config

From the Terraform project you want to scan:

```bash
terradrift config init
```

Edit `terradrift.yaml`:

```yaml
env_file: ".env"
project: "${GCP_PROJECT_ID}"
path: "."
format: "text"
fail_on: "never"
ignore_defaults: false
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
debug: false
```

Create a local `.env` file next to the config:

```bash
GCP_PROJECT_ID=my-project
```

Do not commit `.env` files containing project-specific or secret values.

## 8. Run A Scan

Scan the current Terraform project:

```bash
terradrift scan
```

Scan with explicit arguments:

```bash
terradrift scan --path . --project my-project
```

Write JSON output:

```bash
terradrift scan --path . --project my-project --format json --output terradrift-report.json
```

Scan only specific resource types:

```bash
terradrift scan --path . --project my-project --resource-types google_storage_bucket,google_service_account
```

## 9. Explain A Finding

Each scan writes the last report to `.terradrift/last.json`. Use a finding ID
from the scan output:

```bash
terradrift explain <finding-id>
```

JSON explanation:

```bash
terradrift explain <finding-id> --format json
```

## 10. Run The Included Sample

The repository includes a sample Terraform project in `gcp-test`.

Configure `gcp-test/.env`:

```bash
GCP_PROJECT_ID=my-project
```

Then run from the repository root:

```bash
terradrift scan --config terradrift.yaml
```

The sample config points Terradrift at `gcp-test`.

## Updating

```bash
git pull
go test ./...
go install ./src/cmd/terradrift
```

## Uninstalling

Remove the installed binary from your Go binary directory:

```bash
rm "$(go env GOPATH)/bin/terradrift"
```

You can also remove local scan cache files:

```bash
rm -rf .terradrift
```

## Troubleshooting

- `--project is required for GCP observation`: set `project` in `terradrift.yaml`,
  set `GCP_PROJECT_ID` in `.env`, or pass `--project`.
- `permission denied` or API list failures: rerun
  `scripts/assign_scan_permissions.sh` with a principal that Terradrift uses for
  Application Default Credentials.
- Empty scan result: confirm `path`, `project`, and `resource_types` point to the
  Terraform project and GCP project you intended to compare.
- Command not found after `go install`: add `$(go env GOPATH)/bin` to `PATH`.
