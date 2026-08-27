#!/usr/bin/env bash
set -euo pipefail

source_repository="${SLOPMOCHI_SOURCE_REPOSITORY:-blater/slopmochi}"
tap_repository="${SLOPMOCHI_TAP_REPOSITORY:-blater/homebrew-tap}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_dir="$(cd "$script_dir/../.." && pwd)"

log() {
  printf '==> %s\n' "$*"
}

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required but was not found on PATH."
}

if [[ -z "${HOMEBREW_TAP_TOKEN:-}" ]]; then
  cat >&2 <<'ERROR'
Error: HOMEBREW_TAP_TOKEN is required.

Create a fine-grained GitHub token with Contents: Read and write access to
blater/homebrew-tap, then run:

  ./actions-setup.sh

The token is sent directly to GitHub Actions secrets. It is never printed or
written to the repository.
ERROR
  exit 1
fi

require_command gh
cd "$project_dir"
[[ -f .github/workflows/release.yml ]] || \
  die "Required workflow file is missing: .github/workflows/release.yml"

log "Checking GitHub CLI authentication"
gh auth status --hostname github.com >/dev/null

actual_repository="$(gh repo view "$source_repository" --json nameWithOwner --jq .nameWithOwner)"
[[ "$actual_repository" == "$source_repository" ]] || \
  die "Expected source repository $source_repository, found $actual_repository."

log "Checking Homebrew token access to $tap_repository"
tap_can_push="$(
  GH_TOKEN="$HOMEBREW_TAP_TOKEN" \
    gh api "repos/$tap_repository" --jq '.permissions.push // false'
)" || die "HOMEBREW_TAP_TOKEN cannot access $tap_repository."
[[ "$tap_can_push" == "true" ]] || \
  die "HOMEBREW_TAP_TOKEN needs Contents: Read and write access to $tap_repository."

log "Enabling GitHub Actions for $source_repository"
gh api \
  --method PUT \
  "repos/$source_repository/actions/permissions" \
  -F enabled=true \
  -f allowed_actions=all \
  --silent

log "Storing HOMEBREW_TAP_TOKEN as an Actions secret"
printf '%s' "$HOMEBREW_TAP_TOKEN" |
  gh secret set HOMEBREW_TAP_TOKEN --repo "$source_repository" --app actions

log "Verifying release workflow"
gh workflow view release.yml --repo "$source_repository" >/dev/null
actions_enabled="$(
  gh api "repos/$source_repository/actions/permissions" --jq '.enabled'
)"
[[ "$actions_enabled" == "true" ]] || die "GitHub Actions is not enabled."

log "GitHub Actions setup complete"
printf '%s\n' \
  "Workflow: .github/workflows/release.yml" \
  "Release trigger: push a vX.Y.Z tag or dispatch Release with an existing tag." \
  "Homebrew publisher: $tap_repository"
