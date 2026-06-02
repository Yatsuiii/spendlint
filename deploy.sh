#!/usr/bin/env bash
# Deploy spendlint to Cloud Run.
# Run this once after `gcloud auth login` and `gcloud auth configure-docker us-central1-docker.pkg.dev`.
set -euo pipefail

PROJECT="project-02b2181a-204b-4470-9cc"
REGION="us-central1"
SERVICE="spendlint"
REPO="us-central1-docker.pkg.dev/${PROJECT}/spendlint"
IMAGE="${REPO}/spendlint:latest"

# Create Artifact Registry repo if it doesn't exist
gcloud artifacts repositories create spendlint \
  --repository-format=docker \
  --location="${REGION}" \
  --project="${PROJECT}" 2>/dev/null || true

# Build and push with Cloud Build
gcloud builds submit \
  --project="${PROJECT}" \
  --region="${REGION}" \
  --tag="${IMAGE}" \
  .

# Deploy to Cloud Run (secrets passed as plain env vars - fine for a hackathon demo)
gcloud run deploy "${SERVICE}" \
  --project="${PROJECT}" \
  --region="${REGION}" \
  --image="${IMAGE}" \
  --platform=managed \
  --allow-unauthenticated \
  --port=8080 \
  --memory=512Mi \
  --cpu=1 \
  --timeout=300 \
  --min-instances=1 \
  --max-instances=1 \
  --no-cpu-throttling \
  --add-volume=name=db,type=cloud-storage,bucket=spendlint-db \
  --add-volume-mount=volume=db,mount-path=/data \
  --set-env-vars="GOOGLE_CLOUD_PROJECT=${PROJECT},SPENDLINT_DB=/data/spendlint.db,GITLAB_MCP_URL=${GITLAB_MCP_URL:-https://gitlab.com/api/v4/mcp}" \
  --command="spendlint" \
  --args="serve"

echo ""
echo "Service URL:"
gcloud run services describe "${SERVICE}" \
  --project="${PROJECT}" \
  --region="${REGION}" \
  --format="value(status.url)"
