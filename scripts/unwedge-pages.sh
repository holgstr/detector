#!/usr/bin/env bash
# Clear stuck GitHub Pages deployments that block new publishes.
# Requires: gh auth with repo write (your user token, not the bot).
set -euo pipefail
REPO="${1:-holgstr/detector}"

echo "Listing github-pages deployments..."
gh api "repos/$REPO/deployments?environment=github-pages&per_page=20" \
  --jq '.[] | "\(.id)\t\(.sha[0:7])\t\(.created_at)"'

echo
echo "Canceling Pages deployments by SHA (ignores 404)..."
for sha in $(gh api "repos/$REPO/deployments?environment=github-pages&per_page=20" --jq '.[].sha'); do
  echo -n "  cancel $sha: "
  gh api -X POST "repos/$REPO/pages/deployments/$sha/cancel" >/dev/null 2>&1 \
    && echo ok || echo "(already gone / no access)"
done

echo
echo "Deleting deployment records..."
for id in $(gh api "repos/$REPO/deployments?environment=github-pages&per_page=20" --jq '.[].id'); do
  echo -n "  delete $id: "
  # inactive status first (required before delete when deployment is active)
  gh api -X POST "repos/$REPO/deployments/$id/statuses" -f state=inactive >/dev/null 2>&1 || true
  gh api -X DELETE "repos/$REPO/deployments/$id" >/dev/null 2>&1 \
    && echo ok || echo "(skip)"
done

echo
echo "Requesting a fresh Pages build..."
gh api -X POST "repos/$REPO/pages/builds" && echo "Build requested."

echo
gh api "repos/$REPO/pages" --jq '"status=\(.status) url=\(.html_url) source=\(.source.branch):\(.source.path)"'
