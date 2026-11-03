# Upstream sync and contribute-back

This fork (`socotra/venom`) tracks [ovh/venom](https://github.com/ovh/venom) while carrying Socotra-specific features (reporting, metrics, DPN helpers, release automation).

## Branch model

| Branch | Role |
|--------|------|
| `master` | Clean mirror of `ovh/venom` `master`. Never commit here; only fast-forward from upstream. |
| `internal/develop` | Protected day-to-day branch. Feature work and reviewed upstream syncs land here. |
| `internal/master` | Release branch. Pushing here tags a release via `.github/workflows/tag-release.yml`. |
| `sync/upstream-YYYYMMDD` | Temporary branch for merging `master` into `internal/develop`. |
| `upstream/<name>` | Changes we intend to contribute back to ovh. Based on `master` only. |
| `feature/*`, `fix/*` | Private / fork work based on `internal/develop`. |

```
ovh/venom master  --ff-->  master  --PR-->  internal/develop  --release-->  internal/master
                              |
                              +-- upstream/<name> --PR--> ovh/venom
                                                    --PR--> internal/develop
```

## Day-to-day commands

```bash
# How far behind are we?
make upstream-status

# Pull upstream into master and open a local sync branch
make upstream-sync
# After resolving conflicts / tests:
git push -u origin HEAD
gh pr create --base internal/develop --head "$(git branch --show-current)"

# Or push as part of the sync (still no force-push):
make upstream-sync PUSH=1

# Start a change that should go back to ovh
make upstream-feature NAME=my-fix
# ...edit, then sign commits (DCO)...
git commit -s -m "fix: ..."
make upstream-check
```

Equivalent without Make: `./scripts/upstream.sh status|sync|feature|check`.

A weekly GitHub Action (`.github/workflows/sync-upstream.yml`) does the same sync automatically and opens a PR into `internal/develop`. Run it manually via **Actions → Sync upstream → Run workflow**.

## Contributing a change back to ovh

1. `make upstream-feature NAME=short-description` (branches from `master`).
2. Implement the change. Avoid fork-only paths (`reporting/`, `.github/`, `VERSION`, `cmd/venom/metrics-report/`, `executors/http/dpn.go`, etc.).
3. Commit with DCO sign-off: `git commit -s`.
4. `make upstream-check` — must not warn about fork-only paths.
5. Push and open **two** PRs:
   - to `ovh/venom` `master` (upstream contribution)
   - to `socotra/venom` `internal/develop` (land it in our fork)
6. When upstream merges your PR, the next sync brings it into `master` and then `internal/develop` as a no-op merge for that change.

Private work that must not leave the fork stays on `feature/*` / `fix/*` from `internal/develop`.

## Resolving sync conflicts

Hotspots when merging `master` into `internal/develop`:

- `executors/http/http.go` — upstream multi-value headers / multipart fields vs our DPN / local edits
- `go.mod` / `go.sum` — prefer reconciling with `go mod tidy` after resolving `go.mod`
- `venom.go`, `process*.go` — our reporting hooks vs upstream refactors

Workflow:

```bash
make upstream-sync          # leaves conflicted merge in progress if needed
# edit conflicted files, remove markers
git add <files>
git commit                  # completes the merge
make test
git push -u origin HEAD
```

If the Action opened a draft PR labeled `needs-manual-merge`, check out that branch, finish the merge, push, mark the PR ready, and remove the label.

## One-time: reset `master` to a clean mirror

`master` historically accumulated GitHub-UI merge commits while staying tree-identical to upstream. Reset it once so future syncs can fast-forward.

**A maintainer must run this** (temporarily allow a force push on `master` if branch protection blocks it):

```bash
# From a clean clone / worktree of socotra/venom
git fetch origin
git fetch https://github.com/ovh/venom.git master:refs/remotes/upstream/master

# Backup the old tip
git branch backup/master-pre-reset origin/master
git push origin backup/master-pre-reset

# Point master at upstream (tree-identical; history becomes clean)
git push --force-with-lease=master:origin/master origin upstream/master:master
```

Verify:

```bash
git fetch origin
git rev-parse origin/master upstream/master   # should match after fetch of upstream
git diff origin/master upstream/master        # empty
make upstream-status
```

Then re-enable branch protection (below) and block force pushes on `master`.

## Recommended branch protection

Apply in GitHub → Settings → Branches (or Rulesets):

**`master`**
- Restrict who can push (CI bot / `CI_GH_TOKEN` actor only, if possible)
- Require linear history / disallow non-fast-forward
- **Block force pushes** (after the one-time reset)
- Do not require PR reviews for the sync bot if pushes are already restricted

**`internal/develop`**
- Require a pull request before merging
- Require status checks: `CI Build and Test` / `build`
- Dismiss stale reviews optional

**`internal/master`**
- Same as `internal/develop` (release PRs from `create-release.yml`)

## Secrets

`CI_GH_TOKEN` must be a PAT (or GitHub App token) that can push to `master` / sync branches and open PRs. The default `GITHUB_TOKEN` cannot open PRs that trigger other workflows.
