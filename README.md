# action-pin 📌

[![CI](https://github.com/emirhan-karaca/action-pin/actions/workflows/ci.yml/badge.svg)](https://github.com/emirhan-karaca/action-pin/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/emirhan-karaca/action-pin.svg)](https://pkg.go.dev/github.com/emirhan-karaca/action-pin)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/emirhan-karaca/action-pin?logo=github)](https://github.com/emirhan-karaca/action-pin/releases)

> **Secure your GitHub Actions CI/CD workflows by pinning third-party actions to immutable commit SHAs — without breaking your formatting or losing your comments.**

---

## Why Pin Actions?

In GitHub Actions, referencing actions by mutable tags (e.g., `uses: actions/checkout@v4` or `@main`) poses a serious **supply chain attack risk**:

1. **Tag Hijacking**: If a maintainer's account or repo is compromised, an attacker can move the `v4` tag to malicious code.
2. **Untracked Code Drift**: An author may push bug fixes or breaking changes under the same tag, causing unexpected CI failures.
3. **OpenSSF & SLSA Compliance**: Security frameworks like OpenSSF Scorecard and SLSA mandate pinning all CI dependencies to full 40-character commit hashes.

### The Problem With Existing Pinning Tools
Traditional regex-based or naive YAML re-formatters strip comments, reorder dictionary keys, collapse multi-line scripts, or mangle custom indentation.

### How `action-pin` Solves It
- 🧠 **AST-Preserving**: Powered by `gopkg.in/yaml.v3` node traversal. Comments, spacing, and quotes remain strictly intact.
- 💬 **Human-Readable Annotations**: Automatically appends the original tag name as a comment: `# <tag> [pinned by action-pin]`.
- ⚡ **Zero-Config & Resilient**: Resolves refs via GitHub REST API (supporting `GITHUB_TOKEN`), with an automated fallback to `git ls-remote` when rate-limited.
- 🔁 **Fully Idempotent**: Safe to run on every commit or in pre-commit hooks.

---

## Visual Diff: Before & After

```diff
  jobs:
    build:
      runs-on: ubuntu-latest
      steps:
        # Step 1: Check out source code
-       - uses: actions/checkout@v4
+       - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4 [pinned by action-pin]

        # Step 2: Restore cache from subpath
-       - uses: actions/cache/restore@v3
+       - uses: actions/cache/restore@dacf3200ff73ea515d96a79eefffa1cc3a0bfa99 # v3 [pinned by action-pin]

        # Local & Docker actions remain untouched!
        - uses: ./.github/actions/setup-local
        - uses: docker://alpine:3.18
```

---

## 30-Second Quickstart

### Installation

#### Pre-built Binaries (Linux, macOS, Windows)
Download the latest binary for your operating system and architecture from [GitHub Releases](https://github.com/emirhan-karaca/action-pin/releases).

#### Via Go Install
```bash
go install github.com/emirhan-karaca/action-pin/cmd/action-pin@latest
```

---

## CLI Usage

### Check Mode (CI Friendly)
Verifies whether any workflow contains unpinned actions. Returns exit code `1` if unpinned actions exist:

```bash
action-pin --check
```

Output:
```text
[UNPINNED] .github/workflows/ci.yml:14: actions/checkout@v4 (suggested: 11d5960a326750d5838078e36cf38b85af677262)

Check failed: Found 1 unpinned action(s) across 1 file(s).
Run 'action-pin --fix --dir .github/workflows' to pin them automatically.
```

### Fix Mode (In-Place Update)
Rewrites workflows in place, resolving tags to immutable 40-character commit SHAs:

```bash
action-pin --fix
```

Output:
```text
[PINNED]   .github/workflows/ci.yml:14: actions/checkout@v4 -> 11d5960a326750d5838078e36cf38b85af677262

Success: Pinned 1 action(s) across 1 file(s) (Checked 1 file(s))
```

### CLI Flags Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--check` | `false` | Check for unpinned actions (exits with code 1 if unpinned actions exist) |
| `--fix` | `false` | Fix workflows in place by pinning actions to commit SHAs |
| `--dir` | `.github/workflows` | Directory containing workflow files |
| `--file` | `""` | Target a single workflow file (e.g. `--file .github/workflows/deploy.yml`) |
| `--token` | `$GITHUB_TOKEN` | GitHub Personal Access Token (checks `--token`, `$GITHUB_TOKEN`, or `$GH_TOKEN`) |
| `--verbose` | `false` | Enable verbose logging of inspected actions |
| `--version` | `false` | Print version and build information |

> **Note**: If invoked without flags, `action-pin` runs in check mode by default (`--check --dir .github/workflows`).

---

## GitHub Action Integration (1-Line CI)

Integrate `action-pin` directly into your CI pipeline using the official Composite Action:

```yaml
name: Security & Pinning Check

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

permissions:
  contents: read

jobs:
  verify-pinned-actions:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repository
        uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4 [pinned by action-pin]

      - name: Verify all actions are pinned
        uses: emirhan-karaca/action-pin@19eaee268dfef12c6a0840b284e3a95eb9c0a68c # v1.0.0 [pinned by action-pin]
```

> **Tip**: You can initially add it as `uses: emirhan-karaca/action-pin@v1` and then run `action-pin --fix` to pin it to an immutable commit SHA!

### Action Inputs

| Input | Default | Description |
|-------|---------|-------------|
| `check` | `'true'` | Fail workflow if unpinned actions are found |
| `fix` | `'false'` | Automatically fix and pin actions in place |
| `dir` | `'.github/workflows'` | Directory containing workflow files |
| `token` | `${{ github.token }}` | GitHub token to avoid API rate limits |
| `version` | `'latest'` | Version of action-pin binary to download |

---

## Edge Cases Handled Out-of-the-Box

- **Annotated Tags vs Lightweight Tags**: Correctly dereferences annotated tag objects (`refs/tags/v1^{}`) to the exact commit SHA.
- **Repository Subpaths**: Fully supports nested paths like `actions/cache/restore@v3` and `aws-actions/amazon-ecr-login/.github/workflows/shared.yml@v1`.
- **Local Actions**: Ignores relative paths like `./.github/actions/my-action`.
- **Docker Actions**: Ignores `docker://` container actions.
- **Reusable Workflows**: Seamlessly pins calls to external reusable workflows.
- **Offline / Rate-Limit Fallback**: Automatically invokes `git ls-remote` when GitHub API limits or errors occur.
- **Performance Caching**: Caches resolved SHAs in-memory during execution to eliminate duplicate network calls.

---

## Contributing

We love community contributions! Please read our [CONTRIBUTING.md](CONTRIBUTING.md) guide for details on development setup, running tests, and submitting PRs.

---

## License

`action-pin` is distributed under the terms of the [Apache License 2.0](LICENSE).
