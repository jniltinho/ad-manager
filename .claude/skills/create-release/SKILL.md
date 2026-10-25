---
name: create-release
description: Cut a release of ad-manager on GitHub with the gh CLI — CHANGELOG, checks, tag, the release workflow (Linux, Windows and macOS packages), its verification and the Docker Hub image (jniltinho/ad-manager). Use when asked to release, tag or publish a version.
---

# Releasing ad-manager (GitHub, gh CLI)

The repository is **github.com/jniltinho/ad-manager**. A release is: bump `CHANGELOG.md` and the desktop's version,
commit, push `main`, wait for the CI, push the tag. The tag starts `.github/workflows/release.yml`, which runs the CI
again (server and desktop), builds every package — the server for linux amd64 and the `.deb`, the desktop app for
Linux, Windows and macOS (universal `.app`) — and publishes them in one release with `gh release create`, with the
notes of that version's section in `CHANGELOG.md` and one `checksums.txt`.

> The server's version comes from the **git tag** (`Makefile`: `git describe --tags` → ldflags into
> `internal/buildinfo`). The desktop app has the **same version**, written in `desktop/Cargo.toml`
> (`[workspace.package] version`): set it to X.Y.Z in the release commit (the release workflow refuses a tag that
> does not match it).

Release only when the user asks for it.

## Facts

```bash
gh auth status                                  # logged in, with access to jniltinho/ad-manager
REPO=jniltinho/ad-manager
LAST=$(git describe --tags --abbrev=0 2>/dev/null || git rev-list --max-parents=0 HEAD)   # last tag, or the first commit
```

Versioning (0.x, tags with `v`): a user-visible feature bumps the **minor** (`v0.14.0` → `v0.15.0`); fixes, docs or
CI only bump the **patch** (`v0.14.0` → `v0.14.1`).

## Process

### 1. Prerequisites

```bash
git checkout main && git pull --ff-only
git status                                      # clean, besides the release changes
```

### 2. What changed

```bash
git log "$LAST"..HEAD --oneline
```

### 3. `CHANGELOG.md`

Keep a Changelog, in English. Move the items of `## [Unreleased]` into a new section **above** the previous one —
the release notes are taken from it, by its exact heading:

```markdown
## [Unreleased]

—

## [0.15.0] — YYYY-MM-DD

**One or two lines of summary.**

### Added
### Changed
### Fixed
### Security
```

Omit empty sections.

### 4. The desktop's version, check, commit, push `main`, wait for the CI

```bash
sed -i '0,/^version = ".*"/s//version = "X.Y.Z"/' desktop/Cargo.toml      # [workspace.package]: the tag's version
(cd desktop && scripts/cargo-docker.sh cargo update --workspace)  # Cargo.lock follows (in Docker)
make check && make test && make desktop-check && make build
git add -A && git diff --cached --name-only        # nothing from .run/, dist/, web/dist, .env, *.db
git commit -m "release: vX.Y.Z"
git push origin main
SHA=$(git rev-parse HEAD)
until RUN=$(gh run list --repo "$REPO" --workflow ci.yml --commit "$SHA" --limit 1 --json databaseId -q '.[0].databaseId') && [ -n "$RUN" ]; do sleep 5; done
gh run watch --repo "$REPO" --exit-status "$RUN"
```

Never tag a commit whose CI is red.

### 5. Tag (starts the release workflow)

```bash
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
until RUN=$(gh run list --repo "$REPO" --workflow release.yml --commit "$SHA" --event push --limit 1 --json databaseId -q '.[0].databaseId') && [ -n "$RUN" ]; do sleep 5; done
gh run watch --repo "$REPO" --exit-status "$RUN"    # ~15 min: CI, linux, macos, publish
```

If a job fails: `gh run view --repo "$REPO" "$RUN" --log-failed`, fix on `main`, and release a new patch version
(do not move a published tag).

### 6. Verify

```bash
gh release view vX.Y.Z --repo "$REPO" --json name,assets -q '.name, (.assets[].name)'
```

Expect: `ad-manager_vX.Y.Z_linux_amd64.tar.gz`, `ad-manager_X.Y.Z_amd64.deb`,
`ad-manager-desktop_X.Y.Z_{linux_x86_64.tar.gz,windows_x86_64.zip,macos_universal.zip}` and `checksums.txt`
(the server and the desktop app in one release; the Fyne app in `archive/desktop-go/` is archived and no longer released).

### 6b. Format the GitHub release notes (rustXterm style)

The workflow publishes with the raw `CHANGELOG.md` section. Keep `CHANGELOG.md` in Keep a Changelog, but give the
**GitHub release** the project's release-notes style (same shape as `github.com/jniltinho/rustXterm/releases`):
a `# Release vX.Y.Z` title, emoji sections, a `## 📦 Downloads` list and a `**Full Changelog**` compare link. Map the
CHANGELOG section's content: `### Added` → `## ✨ New Features`, `### Changed`/`### Fixed` → `## 🔧 Improvements`,
`### Security` → `## 🔒 Security` (keep the bullets as written; omit empty sections; drop the one-line summary). Build
the Downloads list from the published assets, and the compare link from the previous tag.

```markdown
# Release vX.Y.Z

## ✨ New Features
- …(from ### Added)

## 🔧 Improvements
- …(from ### Changed / ### Fixed)

## 🔒 Security
- …(from ### Security)

## 📦 Downloads
- `ad-manager_vX.Y.Z_linux_amd64.tar.gz` — server for Linux (x86_64)
- `ad-manager_X.Y.Z_amd64.deb` — server package for Ubuntu 26.04
- `ad-manager-desktop_X.Y.Z_linux_x86_64.tar.gz` — desktop app for Linux
- `ad-manager-desktop_X.Y.Z_windows_x86_64.zip` — desktop app for Windows
- `ad-manager-desktop_X.Y.Z_macos_universal.zip` — desktop app for macOS (universal: Apple Silicon and Intel)
- `checksums.txt` — SHA-256 checksums of every file above

**Full Changelog**: https://github.com/jniltinho/ad-manager/compare/$LAST...vX.Y.Z
```

Write it to `notes.md` and apply: `gh release edit vX.Y.Z --repo "$REPO" --notes-file notes.md` (then `rm notes.md`;
never commit it).

### 7. Docker image (every release)

Publish the all-in-one image of the tag to Docker Hub with every release, built from the tag itself so the version
is exactly `X.Y.Z` (the working tree may already be past it):

```bash
docker info 2>/dev/null | grep -q 'Username: jniltinho' || echo 'docker login first'
W=$(mktemp -d) && git worktree add -q --detach "$W" vX.Y.Z
(cd "$W" && make docker-push)   # jniltinho/ad-manager:X.Y.Z and :latest
git worktree remove --force "$W"
curl -s "https://hub.docker.com/v2/repositories/jniltinho/ad-manager/tags?page_size=2" | python3 -c 'import json,sys; [print(t["name"], t["digest"][:19]) for t in json.load(sys.stdin)["results"]]'
```

Expect `latest` and `X.Y.Z` with the same digest.

## Guardrails

- Never push a tag before check, tests and the CI of that commit are green.
- Never commit secrets, `.run/`, `dist/`, `web/dist`, `.env` or databases.
- The tag is the version; to fix a published release, cut a new patch.
- A release is not done until `jniltinho/ad-manager:X.Y.Z` and `:latest` are on Docker Hub.
