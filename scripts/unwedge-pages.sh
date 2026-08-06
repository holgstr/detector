#!/usr/bin/env bash
# Clear stuck GitHub Pages deployments and switch the site to Actions source.
# Requires: gh auth with repo admin / pages write (your user token, not the bot).
set -euo pipefail
REPO="${1:-holgstr/detector}"

echo "Canceling pages-build-deployment runs..."
for id in $(gh run list --repo "$REPO" --workflow=pages-build-deployment --status in_progress --json databaseId --jq '.[].databaseId' 2>/dev/null || true); do
  echo "  cancel run $id"
  gh run cancel "$id" --repo "$REPO" >/dev/null 2>&1 || true
done

echo "Canceling Pages deployments by SHA..."
for sha in $(gh api "repos/$REPO/deployments?environment=github-pages&per_page=50" --jq '.[].sha'); do
  echo -n "  cancel $sha: "
  gh api -X POST "repos/$REPO/pages/deployments/$sha/cancel" >/dev/null 2>&1 \
    && echo ok || echo "(already gone / no access)"
done

echo "Deleting deployment records..."
for id in $(gh api "repos/$REPO/deployments?environment=github-pages&per_page=50" --jq '.[].id'); do
  echo -n "  delete $id: "
  gh api -X POST "repos/$REPO/deployments/$id/statuses" -f state=inactive >/dev/null 2>&1 || true
  gh api -X DELETE "repos/$REPO/deployments/$id" >/dev/null 2>&1 \
    && echo ok || echo "(skip)"
done

echo "Switching Pages to Actions (build_type=workflow)..."
if ! gh api -X PUT "repos/$REPO/pages" -f build_type=workflow; then
  echo "PUT failed; delete + recreate"
  gh api -X DELETE "repos/$REPO/pages" >/dev/null 2>&1 || true
  sleep 5
  gh api -X POST "repos/$REPO/pages" -f build_type=workflow
fi

echo
gh api "repos/$REPO/pages" --jq '"status=\(.status) build_type=\(.build_type) url=\(.html_url)"'
echo "Done. Trigger Deploy site (Actions → Deploy site → Run workflow) or push to docs/."
