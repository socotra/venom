#!/usr/bin/env bash
# Helpers for syncing with ovh/venom and contributing changes back.
# Safe by default: no force pushes; push only when UPSTREAM_PUSH=1 (or --push).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

UPSTREAM_URL="${UPSTREAM_URL:-https://github.com/ovh/venom.git}"
ORIGIN_REMOTE="${ORIGIN_REMOTE:-origin}"
UPSTREAM_REMOTE="${UPSTREAM_REMOTE:-upstream}"
MASTER_BRANCH="${MASTER_BRANCH:-master}"
DEVELOP_BRANCH="${DEVELOP_BRANCH:-internal/develop}"

# Paths that are private to this fork and should not appear on upstream/* branches.
FORK_ONLY_PATHS=(
  "reporting/"
  ".github/"
  "VERSION"
  "cmd/venom/metrics-report/"
  "executors/http/dpn.go"
  "executors/http/dpn_test.go"
  "docs/UPSTREAM.md"
  "scripts/upstream.sh"
  ".build/upstream.mk"
)

usage() {
  cat <<EOF
Usage: $(basename "$0") <command> [args]

Commands:
  status              Fetch remotes and show how far master / internal/develop
                      are from upstream and each other.
  sync [--push]       Fast-forward master from upstream, create
                      sync/upstream-YYYYMMDD from internal/develop, merge master.
                      Pass --push (or set UPSTREAM_PUSH=1) to push the sync branch.
  feature <name>      Create upstream/<name> from master (no private code).
  check [branch]      Show commits/files vs master and warn if fork-only paths
                      are touched. Defaults to the current branch.

Environment:
  UPSTREAM_URL        Default: ${UPSTREAM_URL}
  UPSTREAM_PUSH=1     Allow git push (same as --push)
  ORIGIN_REMOTE       Default: origin
  UPSTREAM_REMOTE     Default: upstream
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

info() {
  echo "$*"
}

ensure_clean() {
  if [ -n "$(git status --porcelain)" ]; then
    die "working tree is dirty; commit or stash changes first"
  fi
}

ensure_remotes() {
  if ! git remote get-url "${ORIGIN_REMOTE}" >/dev/null 2>&1; then
    die "remote '${ORIGIN_REMOTE}' is not configured"
  fi
  if git remote get-url "${UPSTREAM_REMOTE}" >/dev/null 2>&1; then
    current="$(git remote get-url "${UPSTREAM_REMOTE}")"
    if [ "${current}" != "${UPSTREAM_URL}" ]; then
      git remote set-url "${UPSTREAM_REMOTE}" "${UPSTREAM_URL}"
    fi
  else
    git remote add "${UPSTREAM_REMOTE}" "${UPSTREAM_URL}"
  fi
}

fetch_all() {
  ensure_remotes
  info "Fetching ${ORIGIN_REMOTE} and ${UPSTREAM_REMOTE}..."
  git fetch --prune "${ORIGIN_REMOTE}"
  git fetch --prune "${UPSTREAM_REMOTE}"
}

want_push() {
  if [ "${UPSTREAM_PUSH:-0}" = "1" ]; then
    return 0
  fi
  for arg in "$@"; do
    if [ "${arg}" = "--push" ]; then
      return 0
    fi
  done
  return 1
}

cmd_status() {
  fetch_all

  local master_ref origin_master upstream_master develop_ref
  master_ref="${ORIGIN_REMOTE}/${MASTER_BRANCH}"
  origin_master="${master_ref}"
  upstream_master="${UPSTREAM_REMOTE}/${MASTER_BRANCH}"
  develop_ref="${ORIGIN_REMOTE}/${DEVELOP_BRANCH}"

  for ref in "${origin_master}" "${upstream_master}" "${develop_ref}"; do
    if ! git rev-parse --verify "${ref}" >/dev/null 2>&1; then
      die "missing ref ${ref}; fetch failed or branch does not exist"
    fi
  done

  local master_ahead master_behind develop_behind develop_ahead
  master_ahead=$(git rev-list --count "${upstream_master}..${origin_master}")
  master_behind=$(git rev-list --count "${origin_master}..${upstream_master}")
  develop_behind=$(git rev-list --count "${develop_ref}..${origin_master}")
  develop_ahead=$(git rev-list --count "${origin_master}..${develop_ref}")

  echo
  echo "Branch status"
  echo "-------------"
  printf "  %-28s %s\n" "${ORIGIN_REMOTE}/${MASTER_BRANCH}:" "$(git rev-parse --short "${origin_master}")"
  printf "  %-28s %s\n" "${UPSTREAM_REMOTE}/${MASTER_BRANCH}:" "$(git rev-parse --short "${upstream_master}")"
  printf "  %-28s %s\n" "${ORIGIN_REMOTE}/${DEVELOP_BRANCH}:" "$(git rev-parse --short "${develop_ref}")"
  echo
  printf "  master vs upstream/master:  %s ahead, %s behind\n" "${master_ahead}" "${master_behind}"
  printf "  internal/develop vs master: %s ahead, %s behind\n" "${develop_ahead}" "${develop_behind}"
  echo

  if [ "${master_ahead}" -ne 0 ]; then
    echo "  WARNING: master has commits not in upstream/master."
    echo "           After the one-time reset it should only fast-forward."
    echo "           See docs/UPSTREAM.md (master reset)."
    echo
  fi
  if [ "${master_behind}" -ne 0 ]; then
    echo "  Action: run 'make upstream-sync' (or scripts/upstream.sh sync) to pull upstream."
    echo
  fi
  if [ "${develop_behind}" -ne 0 ]; then
    echo "  Action: sync PR needed — ${develop_behind} commit(s) on master not in internal/develop."
    echo
  fi
  if [ "${master_behind}" -eq 0 ] && [ "${develop_behind}" -eq 0 ] && [ "${master_ahead}" -eq 0 ]; then
    echo "  All good: master mirrors upstream and internal/develop includes master."
    echo
  fi
}

cmd_sync() {
  ensure_clean
  fetch_all

  local origin_master upstream_master develop_ref
  origin_master="${ORIGIN_REMOTE}/${MASTER_BRANCH}"
  upstream_master="${UPSTREAM_REMOTE}/${MASTER_BRANCH}"
  develop_ref="${ORIGIN_REMOTE}/${DEVELOP_BRANCH}"

  info "Updating local ${MASTER_BRANCH} from ${origin_master}..."
  git checkout -B "${MASTER_BRANCH}" "${origin_master}"

  info "Fast-forwarding ${MASTER_BRANCH} from ${upstream_master}..."
  if ! git merge --ff-only "${upstream_master}"; then
    die "cannot fast-forward ${MASTER_BRANCH} from ${upstream_master}. Reset master first (see docs/UPSTREAM.md)."
  fi

  if want_push "$@"; then
    info "Pushing ${MASTER_BRANCH} to ${ORIGIN_REMOTE}..."
    git push "${ORIGIN_REMOTE}" "HEAD:${MASTER_BRANCH}"
  else
    info "Skipped push of ${MASTER_BRANCH} (pass --push or UPSTREAM_PUSH=1 to push)."
  fi

  local date_stamp branch ahead
  date_stamp=$(date -u +%Y%m%d)
  branch="sync/upstream-${date_stamp}"
  ahead=$(git rev-list --count "${develop_ref}..${MASTER_BRANCH}")

  if [ "${ahead}" -eq 0 ]; then
    info "internal/develop already contains ${MASTER_BRANCH}; nothing to sync."
    return 0
  fi

  info "Creating ${branch} from ${develop_ref} (${ahead} commit(s) to merge)..."
  git checkout -B "${branch}" "${develop_ref}"

  local conflict=false
  if git merge --no-edit "${MASTER_BRANCH}"; then
    info "Merge of ${MASTER_BRANCH} into ${branch} succeeded."
  else
    conflict=true
    info "Merge conflicts detected."
    info "Conflicted files:"
    git diff --name-only --diff-filter=U || true
    cat <<EOF

Next steps:
  1. Resolve conflicts (hotspots: executors/http/http.go, go.mod, go.sum, venom.go, process*.go)
  2. git add <resolved files> && git commit
  3. make test
  4. git push -u ${ORIGIN_REMOTE} ${branch}
  5. Open a PR: ${branch} -> ${DEVELOP_BRANCH}
     gh pr create --base ${DEVELOP_BRANCH} --head ${branch} --title "chore: sync upstream into internal/develop"

Leaving the conflicted merge in progress on ${branch}.
EOF
    return 1
  fi

  if want_push "$@"; then
    info "Pushing ${branch} to ${ORIGIN_REMOTE}..."
    git push -u "${ORIGIN_REMOTE}" "HEAD:${branch}"
    info "Open a PR with:"
    info "  gh pr create --base ${DEVELOP_BRANCH} --head ${branch} --title \"chore: sync upstream into internal/develop\""
  else
    cat <<EOF

Next steps:
  1. make test
  2. git push -u ${ORIGIN_REMOTE} ${branch}
     (or re-run with --push / UPSTREAM_PUSH=1)
  3. gh pr create --base ${DEVELOP_BRANCH} --head ${branch} --title "chore: sync upstream into internal/develop"
EOF
  fi

  if [ "${conflict}" = "true" ]; then
    return 1
  fi
}

cmd_feature() {
  local name="${1:-}"
  if [ -z "${name}" ]; then
    die "usage: $(basename "$0") feature <name>"
  fi
  # Strip accidental upstream/ prefix
  name="${name#upstream/}"

  ensure_clean
  fetch_all

  local branch="upstream/${name}"
  local base="${ORIGIN_REMOTE}/${MASTER_BRANCH}"

  if git show-ref --verify --quiet "refs/heads/${branch}"; then
    die "local branch ${branch} already exists"
  fi
  if git ls-remote --exit-code --heads "${ORIGIN_REMOTE}" "${branch}" >/dev/null 2>&1; then
    die "remote branch ${ORIGIN_REMOTE}/${branch} already exists"
  fi

  info "Creating ${branch} from ${base}..."
  git checkout -b "${branch}" "${base}"
  cat <<EOF

Branch ${branch} is ready. This branch is based on master (upstream mirror)
so it can be contributed back to ovh/venom without private fork commits.

  # develop + sign commits (DCO required by upstream)
  git commit -s -m "..."

  # open PR to ovh/venom
  git push -u ${ORIGIN_REMOTE} ${branch}
  gh pr create --repo ovh/venom --base master --head socotra/venom:${branch}

  # also land in our fork
  gh pr create --base ${DEVELOP_BRANCH} --head ${branch}

  # sanity-check before opening PRs
  make upstream-check
EOF
}

cmd_check() {
  local branch="${1:-}"
  if [ -z "${branch}" ]; then
    branch="$(git rev-parse --abbrev-ref HEAD)"
  fi

  fetch_all

  local base="${ORIGIN_REMOTE}/${MASTER_BRANCH}"
  if ! git rev-parse --verify "${branch}" >/dev/null 2>&1; then
    die "unknown branch ${branch}"
  fi

  echo
  echo "Commits on ${branch} not in ${base}:"
  echo "-------------------------------------"
  if [ "$(git rev-list --count "${base}..${branch}")" -eq 0 ]; then
    echo "  (none)"
  else
    git log --oneline "${base}..${branch}"
  fi
  echo
  echo "Files changed vs ${base}:"
  echo "-------------------------"
  git diff --name-status "${base}...${branch}" || true
  echo

  local bad=()
  local changed
  changed=$(git diff --name-only "${base}...${branch}" || true)
  local path file
  for path in "${FORK_ONLY_PATHS[@]}"; do
    while IFS= read -r file; do
      [ -z "${file}" ] && continue
      case "${file}" in
        ${path}*) bad+=("${file}") ;;
      esac
      if [ "${file}" = "${path%/}" ]; then
        bad+=("${file}")
      fi
    done <<<"${changed}"
  done

  # de-dupe
  if [ "${#bad[@]}" -gt 0 ]; then
    local uniq=()
    local b u seen
    for b in "${bad[@]}"; do
      seen=0
      for u in "${uniq[@]+"${uniq[@]}"}"; do
        if [ "${u}" = "${b}" ]; then seen=1; break; fi
      done
      if [ "${seen}" -eq 0 ]; then
        uniq+=("${b}")
      fi
    done

    echo "WARNING: branch touches fork-only paths (do not send these upstream):"
    for b in "${uniq[@]}"; do
      echo "  - ${b}"
    done
    echo
    echo "Rebase onto ${MASTER_BRANCH} and drop private commits, or move private"
    echo "changes to a feature/* branch based on ${DEVELOP_BRANCH}."
    return 1
  fi

  if [[ "${branch}" != upstream/* ]]; then
    echo "Note: branch is not named upstream/<name>. Prefer that prefix for contribute-back work."
    echo
  fi

  echo "No fork-only paths detected. Branch looks safe to propose upstream."
  echo
}

main() {
  local cmd="${1:-}"
  if [ -z "${cmd}" ]; then
    usage
    exit 1
  fi
  shift || true

  case "${cmd}" in
    status)  cmd_status "$@" ;;
    sync)    cmd_sync "$@" ;;
    feature) cmd_feature "$@" ;;
    check)   cmd_check "$@" ;;
    -h|--help|help) usage ;;
    *)
      usage
      die "unknown command: ${cmd}"
      ;;
  esac
}

main "$@"
