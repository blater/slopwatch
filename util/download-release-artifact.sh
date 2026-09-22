#!/usr/bin/env bash
# Retrieve tested bytes only. This script never starts CI or executes the bundle.
set -euo pipefail
cd "$(dirname "$0")/.."

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}
version="${1:-}"
[[ $# == 1 && "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || \
  die 'Usage: ./util/download-release-artifact.sh X.Y.Z'
repository="${SLOPWATCH_SOURCE_REPOSITORY:-${GITHUB_REPOSITORY:-blater/slopwatch}}"
sha="$(git rev-parse HEAD)"
artifact_name="slopwatch-validated-$sha"
recovery="Run CI explicitly on a branch/tag at $sha, wait for success, then retry Release. Deployment never builds."

# Server-side SHA filtering plus local event/SHA checks exclude PR merge artifacts.
# Paginate so older successful runs remain eligible after failed retries.
appearance_deadline=$((SECONDS + 60))
completion_deadline=$((SECONDS + 1800))
while :; do
  runs="$(gh api --method GET "repos/$repository/actions/workflows/ci.yml/runs" \
    -f head_sha="$sha" -f per_page=100 --paginate --slurp)" || \
    die "Cannot inspect CI runs for $sha. Check Actions read permission."
  eligible="$(jq --arg sha "$sha" '[.[].workflow_runs[] | select(.head_sha == $sha and (.event == "push" or .event == "workflow_dispatch"))] | sort_by(.run_number, .run_attempt) | reverse' <<< "$runs")"
  selected="$(jq -c '[.[] | select(.status == "completed" and .conclusion == "success")][0] // empty' <<< "$eligible")"
  [[ -z "$selected" ]] || break
  active="$(jq -r '[.[] | select(.status != "completed")][0].id // empty' <<< "$eligible")"
  if [[ -n "$active" ]]; then
    ((SECONDS < completion_deadline)) || die "Timed out waiting for CI run $active for $sha. $recovery"
    printf 'Waiting for matching CI run %s for %s.\n' "$active" "$sha"
  elif [[ "$(jq length <<< "$eligible")" != 0 ]]; then
    die "Matching CI for $sha has no successful or active run (failed/cancelled). $recovery"
  else
    ((SECONDS < appearance_deadline)) || die "No push/manual CI run appeared for $sha. $recovery"
    printf 'Waiting for push/manual CI to appear for %s.\n' "$sha"
  fi
  sleep 10
done
run_id="$(jq -r .id <<< "$selected")"
# Recheck the selected run immediately before retrieving its artifacts.
run="$(gh api "repos/$repository/actions/runs/$run_id")" || die "Cannot verify CI run $run_id."
jq -e --arg sha "$sha" '.head_sha == $sha and (.event == "push" or .event == "workflow_dispatch") and .status == "completed" and .conclusion == "success"' <<< "$run" >/dev/null || \
  die "CI run $run_id no longer has successful exact-commit provenance. $recovery"
artifacts="$(gh api "repos/$repository/actions/runs/$run_id/artifacts?per_page=100" --paginate --slurp)" || \
  die "Cannot inspect artifacts from CI run $run_id."
matching="$(jq --arg name "$artifact_name" '[.[].artifacts[] | select(.name == $name)]' <<< "$artifacts")"
[[ "$(jq length <<< "$matching")" == 1 ]] || die "Validated artifact $artifact_name is missing or ambiguous in CI run $run_id (possibly expired/deleted). $recovery"
jq -e '.[0].expired == false' <<< "$matching" >/dev/null || die "Validated artifact $artifact_name has expired. $recovery"

artifact_tmp="$(mktemp -d "${TMPDIR:-/tmp}/slopwatch-release.XXXXXX")"
trap 'rm -rf "$artifact_tmp"' EXIT
mkdir -p "$artifact_tmp/dist"
gh run download "$run_id" --repo "$repository" --name "$artifact_name" --dir "$artifact_tmp/dist" || \
  die "Cannot download validated artifact $artifact_name; it may have expired. $recovery"
# The shared build checksum names dist/<archive>, so verify from its parent.
archive_name=slopwatch-dev-darwin-arm64.tar.gz
[[ -f "$artifact_tmp/dist/$archive_name" && ! -L "$artifact_tmp/dist/$archive_name" && -f "$artifact_tmp/dist/SHA256SUMS" && ! -L "$artifact_tmp/dist/SHA256SUMS" ]] || \
  die 'CI artifact is missing its archive or checksum.'
[[ "$(wc -l < "$artifact_tmp/dist/SHA256SUMS" | tr -d ' ')" == 1 ]] && \
  LC_ALL=C grep -Eq '^[0-9a-f]{64}  dist/slopwatch-dev-darwin-arm64\.tar\.gz$' "$artifact_tmp/dist/SHA256SUMS" || \
  die 'CI artifact checksum must identify exactly the expected archive.'
(cd "$artifact_tmp" && shasum -a 256 --check dist/SHA256SUMS) || die 'CI archive checksum mismatch; refusing publication.'
mkdir -p dist
mv "$artifact_tmp/dist/$archive_name" "dist/slopwatch-${version}-darwin-arm64.tar.gz"
shasum -a 256 "dist/slopwatch-${version}-darwin-arm64.tar.gz" > dist/SHA256SUMS
printf 'Retrieved validated archive from CI run %s at %s; archive bytes preserved.\n' "$run_id" "$sha"
