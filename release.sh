#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source_repository="${SLOPWATCH_SOURCE_REPOSITORY:-blater/slopwatch}"
tap_repository="${SLOPWATCH_TAP_REPOSITORY:-blater/homebrew-tap}"

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || \
    die "$1 is required but was not found on PATH."
}

current_release() {
  git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-version:refname \
    | sed -n '1{s/^v//;p;}'
}

usage() {
  printf 'Current release: %s\n\n' "${current_version:-none}"
  cat <<'USAGE'
Usage: ./release.sh X.Y.Z

Tag and push the current clean commit, wait for GitHub Actions to build and
publish the complete SlopWatch bundle, then verify the GitHub release and the
Homebrew tap update.
USAGE
}

version_is_greater() {
  local candidate="$1"
  local current="$2"
  local candidate_parts current_parts index candidate_part current_part
  IFS=. read -r -a candidate_parts <<< "$candidate"
  IFS=. read -r -a current_parts <<< "$current"
  for index in 0 1 2; do
    candidate_part="$((10#${candidate_parts[$index]}))"
    current_part="$((10#${current_parts[$index]}))"
    if ((candidate_part > current_part)); then return 0; fi
    if ((candidate_part < current_part)); then return 1; fi
  done
  return 1
}

cd "$script_dir"
current_version="$(current_release)"

case "$#" in
  0)
    usage
    exit 0
    ;;
  1)
    case "$1" in
      -h|--help)
        usage
        exit 0
        ;;
    esac
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

version="$1"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || \
  die "Version must use X.Y.Z numeric format, for example 1.2.3."
if [[ -n "$current_version" ]]; then
  version_is_greater "$version" "$current_version" || \
    die "Version $version must be greater than current release $current_version."
fi

require_command git
[[ -f .github/workflows/release.yml ]] || die "The GitHub release workflow is missing."
[[ -f util/build_brew ]] || die "The Homebrew publisher is missing."
[[ -z "$(git status --porcelain --untracked-files=normal)" ]] || \
  die "The Git working tree is not clean. Commit or stash existing changes first."

require_command gh
gh auth status --hostname github.com >/dev/null 2>&1 || \
  die "GitHub CLI authentication is required. Run: gh auth login --hostname github.com"

actions_secrets="$(gh secret list --repo "$source_repository" --app actions)" || \
  die "Could not read GitHub Actions secrets for $source_repository."
grep -q '^HOMEBREW_TAP_TOKEN[[:space:]]' <<< "$actions_secrets" || \
  die "HOMEBREW_TAP_TOKEN is not configured for $source_repository. Run ./actions-setup.sh."

branch="$(git branch --show-current)"
[[ -n "$branch" ]] || die "A release cannot be created from a detached HEAD."
tag="v$version"
git rev-parse --verify --quiet "refs/tags/$tag" >/dev/null && \
  die "Tag $tag already exists locally."
remote_tag="$(git ls-remote --tags origin "refs/tags/$tag")" || \
  die "Could not check whether tag $tag exists on origin."
[[ -z "$remote_tag" ]] || die "Tag $tag already exists on origin."

git push origin "$branch"
git tag -a "$tag" -m "Release $tag"
git push origin "$tag"

release_commit="$(git rev-parse "${tag}^{commit}")"
printf 'Release tag %s pushed; waiting for the GitHub release workflow.\n' "$tag"
run_id=""
for _ in {1..30}; do
  run_id="$(
    gh api \
      --method GET \
      "repos/$source_repository/actions/workflows/release.yml/runs" \
      -f event=push \
      -f head_sha="$release_commit" \
      -f per_page=1 \
      --jq '.workflow_runs[0].id // empty'
  )" || die "Could not find the GitHub release workflow run."
  [[ -z "$run_id" ]] || break
  sleep 2
done
[[ -n "$run_id" ]] || die "The GitHub release workflow did not appear for $tag."

gh run watch "$run_id" --repo "$source_repository" --exit-status || \
  die "The release workflow failed: https://github.com/$source_repository/actions/runs/$run_id"

archive="slopwatch-${version}-darwin-arm64.tar.gz"
release_assets="$(
  gh release view "$tag" --repo "$source_repository" --json assets --jq '.assets[].name'
)" || die "Could not inspect GitHub release $tag."
for asset in "$archive" SHA256SUMS; do
  grep -Fxq "$asset" <<< "$release_assets" || \
    die "GitHub release $tag is missing $asset."
done

formula="$(
  gh api "repos/$tap_repository/contents/Formula/slopwatch.rb?ref=main" \
    --jq '.content' | base64 --decode
)" || die "Could not inspect the Homebrew tap formula."
grep -Fq "/releases/download/$tag/$archive" <<< "$formula" || \
  die "The Homebrew tap does not reference $tag."
grep -Fq "version \"$version\"" <<< "$formula" || \
  die "The Homebrew tap does not report version $version."

printf 'Release complete: https://github.com/%s/releases/tag/%s\n' "$source_repository" "$tag"
printf 'Install: brew install blater/tap/slopwatch\n'
