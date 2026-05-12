#!/usr/bin/env bash
set -euo pipefail

PROJECT_INPUT="${1:-}"
MEMBER="${2:-}"

if [[ -z "${PROJECT_INPUT}" || -z "${MEMBER}" ]]; then
  cat <<'USAGE'
Usage:
  scripts/assign_scan_permissions.sh <project-id-or-number> <member>

Arguments:
  <project-id-or-number>  GCP project ID or project number that Terradrift scans
  <member>      IAM principal in gcloud member format, for example:
                user:alice@example.com
                serviceAccount:ci-scanner@my-project.iam.gserviceaccount.com
USAGE
  exit 1
fi

PROJECT_ID="$(gcloud projects describe "${PROJECT_INPUT}" --format='value(projectId)')"
if [[ -z "${PROJECT_ID}" ]]; then
  echo "Unable to resolve project from input: ${PROJECT_INPUT}" >&2
  exit 1
fi

echo "Enabling required APIs in project ${PROJECT_ID}..."
gcloud services enable iam.googleapis.com storage.googleapis.com run.googleapis.com --project "${PROJECT_ID}"

echo "Granting read-only discovery roles to ${MEMBER}..."
gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member "${MEMBER}" \
  --role "roles/viewer" \
  --quiet

gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member "${MEMBER}" \
  --role "roles/iam.serviceAccountViewer" \
  --quiet

gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member "${MEMBER}" \
  --role "roles/run.viewer" \
  --quiet

echo "Ensuring ADC uses ${PROJECT_ID} as quota project (optional if already set)..."
gcloud auth application-default set-quota-project "${PROJECT_ID}" || true

echo "Done. You can now run: terradrift scan"
