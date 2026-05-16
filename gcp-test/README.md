# gcp-test

This Terraform project provisions a small GCP sample stack for Terradrift scans.

It now includes examples for:

- `google_storage_bucket`
- `google_service_account`
- `google_cloud_run_v2_service`
- `google_compute_network`
- `google_compute_subnetwork`
- `google_compute_instance`
- `google_pubsub_topic`
- `google_bigquery_dataset`
- `google_artifact_registry_repository`
- `google_secret_manager_secret`

Notes:

- The Compute Engine sample uses an `e2-micro` VM on a dedicated VPC/subnet so the stack does not depend on a default network.
- The rest of the added samples are usage-based services with low or zero idle cost.
- API enablement is handled in `main.tf` with `google_project_service`.
