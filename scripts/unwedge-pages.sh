#!/usr/bin/env bash
# Clear stuck GitHub Pages deployments. Run as yourself (admin/pages write).
# After this, push a NEW commit — cancelled SHAs cannot be redeployed.
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
    && echo ok || echo "(already gone)"
done

echo "Deleting deployment records..."
for id in $(gh api "repos/$REPO/deployments?environment=github-pages&per_page=50" --jq '.[].id' 2>/dev/null || true); do
  echo -n "  delete $id: "
  gh api -X POST "repos/$REPO/deployments/$id/statuses" -f state=inactive >/dev/null 2>&1 || true
  gh api -X DELETE "repos/$REPO/deployments/$id" >/dev/null 2>&1 && echo ok || echo "(skip)"
done

echo
gh api "repos/$REPO/pages" --jq '"status=\(.status) build_type=\(.build_type) url=\(.html_url)"' || true
echo
echo "Next: push a NEW commit to main (cancelled SHAs cannot be redeployed)."
