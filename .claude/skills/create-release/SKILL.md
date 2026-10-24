---
name: create-release
description: Cut a release of ad-manager on GitHub with the gh CLI — CHANGELOG, checks, tag, the release workflow (Linux, Windows and macOS packages) and its verification. Use when asked to release, tag or publish a version.
---

# Releasing ad-manager (GitHub, gh CLI)

The repository is **github.com/jniltinho/ad-manager**. A release is: bump `CHANGELOG.md`, commit, push `main`, wait
for the CI, push the tag. The tag starts `.github/workflows/release.yml`, which runs the CI again, builds every
package (server linux amd64 and the `.deb`)
and publishes the release with `gh release create`, with the notes of that version's section in `CHANGELOG.md`
and `checksums.txt`.

> The version comes from the **git tag** (`Makefile`: `git describe --tags` → ldflags into `internal/buildinfo`).
> There is no version constant to bump: the tag *is* the version.

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

### 4. Check, commit, push `main`, wait for the CI

```bash
make check && make test && make build
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

Expect: `ad-manager_vX.Y.Z_linux_amd64.tar.gz`, `ad-manager_X.Y.Z_amd64.deb` and `checksums.txt`. The desktop app
(Rust) is released on its own `desktop-rust-vX.Y.Z` tags (`desktop-rust-release.yml`); the Fyne app in `desktop/` is
archived and no longer released.

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
- `checksums.txt` — SHA-256 checksums

**Full Changelog**: https://github.com/jniltinho/ad-manager/compare/$LAST...vX.Y.Z
```

Write it to `notes.md` and apply: `gh release edit vX.Y.Z --repo "$REPO" --notes-file notes.md` (then `rm notes.md`;
never commit it).

### 7. Docker image (optional, when asked)

```bash
make docker-push          # jniltinho/ad-manager:<version> and :latest (docker login first)
```

## Guardrails

- Never push a tag before check, tests and the CI of that commit are green.
- Never commit secrets, `.run/`, `dist/`, `web/dist`, `.env` or databases.
- The tag is the version; to fix a published release, cut a new patch.
