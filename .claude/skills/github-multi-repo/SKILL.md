---
name: github-multi-repo
version: 2.0.0
description: Multi-repository coordination and synchronization using Git and GitHub CLI.
category: github-integration
tags: [multi-repo, synchronization, github, gh-cli]
---

# GitHub Multi-Repository Coordination Skill

Manage changes, synchronize dependencies, and run operations across multiple GitHub repositories.

## Quick Start

### 1. Cloning Multiple Repositories
Use the GitHub CLI to find and clone related repositories:
```bash
# List repositories in an organization matching a topic
gh repo list my-org --topic "microservice" --json name -q '.[].name' > repos.txt

# Clone all matching repositories
while read repo; do
  gh repo clone "my-org/$repo"
done < repos.txt
```

### 2. Executing Cross-Repo Commands
```bash
# Run a command in all cloned repositories
for dir in */; do
  if [ -d "$dir/.git" ]; then
    echo "Processing $dir"
    (cd "$dir" && git fetch origin && git status --short)
  fi
done
```

## Core Capabilities

### 1. Coordinated Dependency Updates
When a shared library is updated, roll out the update across consuming repositories:
```bash
for repo in repo-a repo-b repo-c; do
  cd $repo
  git checkout main
  git pull
  git checkout -b update-shared-lib
  
  # Example: Update npm dependency
  npm install shared-lib@latest
  
  git commit -am "chore: update shared-lib"
  git push -u origin update-shared-lib
  gh pr create --title "Update shared-lib" --body "Automated dependency update" --fill
  cd ..
done
```

### 2. Git Submodules
For tight coupling, use Git submodules to ensure repositories are locked to specific commits.
```bash
# Add a submodule
git submodule add https://github.com/my-org/shared-config.git

# Update all submodules to their latest remote commits
git submodule update --remote --merge
```

### 3. Searching Across Repositories
```bash
# Search code across an entire organization
gh search code "TODO" --owner my-org --extension ts
```

## Best Practices
- **Standardization:** Use consistent branch names and PR titles when orchestrating multi-repo changes.
- **Batching:** When updating many repositories, use scripting to automate branch creation, committing, and PR opening.
- **Review:** Avoid auto-merging cross-repo PRs without CI passing on each individual repository.
