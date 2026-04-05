---
name: github-release-management
version: 2.0.0
description: Comprehensive GitHub release management using standard Git and GitHub CLI (gh) commands for versioning, testing, and deployment.
category: github
tags: [release, deployment, versioning, automation, ci-cd, gh-cli]
---

# GitHub Release Management Skill

Manage software releases, changelogs, and version tags using standard Git and the GitHub CLI (`gh`).

## Quick Start

### 1. Create a Standard Release
```bash
# Tag the current commit
git tag v2.0.0
git push origin v2.0.0

# Create a GitHub release with auto-generated release notes
gh release create v2.0.0 --generate-notes --title "Release v2.0.0"
```

### 2. Create a Draft Release
```bash
gh release create v2.0.1 --draft --title "v2.0.1" --notes "Draft notes here"
```

## Core Capabilities

### 1. Changelog Generation
Use the GitHub CLI to view merged PRs or commits since the last release.
```bash
# Get the latest release tag
LAST_TAG=$(gh release view --json tagName -q .tagName)

# Get PRs merged since the last release
gh pr list --state merged --base main --json number,title,author \
  --search "merged:>$(gh release view $LAST_TAG --json publishedAt -q .publishedAt)"
```

### 2. Multi-Platform Build & Upload
Attach build artifacts to a release.
```bash
# Upload artifacts to an existing release
gh release upload v2.0.0 dist/app-linux-amd64.tar.gz dist/app-macos-arm64.zip
```

### 3. Release Monitoring
```bash
# List recent releases
gh release list --limit 5

# View a specific release
gh release view v2.0.0
```

## Best Practices
- **Semantic Versioning:** Always use vX.Y.Z tags.
- **Automated Notes:** Prefer `--generate-notes` to let GitHub pull in PR titles and contributors automatically.
- **Drafts First:** Always create releases as `--draft` when orchestrating complex deployments, giving you a chance to review notes and upload all binaries before publishing.
