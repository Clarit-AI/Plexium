---
name: github-workflow-automation
version: 2.0.0
category: github
description: GitHub Actions workflow automation, CI/CD pipeline management, and monitoring using the GitHub CLI (gh).
tags: [github, github-actions, ci-cd, workflow-automation, gh-cli]
---

# GitHub Workflow Automation Skill

Manage and monitor GitHub Actions workflows directly from the command line using the GitHub CLI (`gh`).

## Quick Start

### 1. View and Run Workflows
```bash
# List all workflows in the repository
gh workflow list

# Run a specific workflow
gh workflow run "CI Pipeline" --ref main

# Run a workflow with inputs
gh workflow run deploy.yml -f environment=production -f debug=true
```

### 2. Monitor Workflow Runs
```bash
# List recent workflow runs
gh run list --limit 10

# Watch a run in progress (interactive)
gh run watch

# View logs for a specific run (or the last one)
gh run view --log
```

## Core Capabilities

### 1. Analyzing Failures
Quickly find out why a CI job failed.
```bash
# Get the status of the latest run
gh run list --status failure --limit 1

# View the failed job logs
gh run view <run-id> --log-failed
```

### 2. Secret and Variable Management
```bash
# Set a repository secret for Actions
gh secret set API_TOKEN --body "your-token-here"

# Set an environment secret
gh secret set DB_PASSWORD --env production --body "secure-password"

# List variables
gh variable list
```

### 3. Creating New Workflows
When generating new workflows, adhere to these best practices:
- Use `actions/checkout@v4` and `actions/setup-node@v4` (or current latest versions).
- Define clear triggers (`on: push`, `on: pull_request`, `on: workflow_dispatch`).
- Use concurrency groups to cancel redundant runs:
  ```yaml
  concurrency:
    group: ${{ github.workflow }}-${{ github.ref }}
    cancel-in-progress: true
  ```
