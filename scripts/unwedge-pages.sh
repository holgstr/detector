#!/usr/bin/env bash
# Clear stuck GitHub Pages deployments that block new publishes.
# Requires: gh auth with pages write (your user token for Settings changes).
set -euo pipefail
REPO="${1:-holgstr/detector}"

echo "Canceling pages-build-deployment runs..."
for id in $(gh run list --repo "$REPO" --workflow=pages-build-deployment --status in_progress --json databaseId --jq '.[].databaseId' 2>/dev/null || true); do
  echo "  cancel run $id"
  gh run cancel "$id" --repo "$REPO" >/dev/null 2>&1 || true
done

echo "Canceling Pages deployments by SHA..."
for sha in $(gh api "repos/$REPO/deployments?environment=github-pages&per_page=50" --jq '.[].sha' 2>/dev/null || true); do
  echo -n "  cancel $sha: "
  gh api -X POST "repos/$REPO/pages/deployments/$sha/cancel" >/dev/null 2>&1 \
    && echo ok || echo "(already gone / no access)"
done

echo "Deleting deployment records..."
for id in $(gh api "repos/$REPO/deployments?environment=github-pages&per_page=50" --jq '.[].id' 2>/dev/null || true); do
  echo -n "  delete $id: "
  gh api -X POST "repos/$REPO/deployments/$id/statuses" -f state=inactive >/dev/null 2>&1 || true
  gh api -X DELETE "repos/$REPO/deployments/$id" >/dev/null 2>&1 \
    && echo ok || echo "(skip)"
done

echo "Best-effort switch to Actions source (may need admin)..."
gh api -X PUT "repos/$REPO/pages" -f build_type=workflow >/dev/null 2>&1 \
  && echo "switched to workflow" \
  || echo "skipped — set Settings → Pages → Source to GitHub Actions if you want Actions deploys"

echo "Requesting a fresh Pages build..."
gh api -X POST "repos/$REPO/pages/builds" >/dev/null && echo "Build requested." || echo "Build request skipped."

echo
gh api "repos/$REPO/pages" --jq '"status=\(.status) build_type=\(.build_type) url=\(.html_url)"'
