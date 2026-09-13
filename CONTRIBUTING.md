# Contributing to action-pin

Thank you for your interest in contributing to `action-pin`! We welcome contributions of all kinds: bug reports, documentation improvements, feature requests, and code contributions.

---

## Development Environment Setup

### Prerequisites

- **Go**: Version 1.22 or newer ([go.dev/dl](https://go.dev/dl/))
- **Git**: Latest version ([git-scm.com](https://git-scm.com/))
- *(Optional)* **GoReleaser**: For testing packaging builds locally

### Clone and Verify

```bash
git clone https://github.com/emirhan-karaca/action-pin.git
cd action-pin

# Verify dependencies
go mod tidy
go mod verify

# Run test suite
go test -v ./...

# Run linter / vetting
go vet ./...
```

---

## Project Structure

```
action-pin/
├── action.yml               # Composite GitHub Action for CI pipelines
├── .goreleaser.yaml         # Multi-platform release definitions
├── .github/
│   └── workflows/
│       ├── ci.yml           # CI test matrix across OS and Go versions
│       └── release.yml      # Release workflow
├── cmd/
│   └── action-pin/
│       ├── main.go          # CLI entrypoint and flag parsing
│       └── main_test.go     # CLI integration tests
└── internal/
    ├── action/
    │   ├── action.go        # GitHub Action reference parsing
    │   └── action_test.go   # Parsing tests
    ├── resolver/
    │   ├── resolver.go      # GitHub API and Git fallback ref resolution
    │   └── resolver_test.go # Resolver tests (mock API & git fallback)
    └── pinner/
        ├── pinner.go        # YAML AST traversal, line comment preservation
        └── pinner_test.go   # AST preservation and formatting tests
```

---

## Coding Guidelines

1. **AST & Comment Preservation**:
   All YAML manipulations must preserve original comments, indentation, and structure using `gopkg.in/yaml.v3` `yaml.Node` traversal. Avoid string-replace regex hacks on raw YAML files.

2. **Idempotence**:
   Running `action-pin --fix` on already-pinned workflows must be a no-op and produce zero diffs.

3. **Fallback & Resiliency**:
   The resolver must gracefully handle GitHub API rate limits by falling back to `git ls-remote`.

4. **Testing**:
   - Every bug fix or new feature must include accompanying unit tests.
   - Run `go test -v ./...` before submitting a pull request.
   - Run `go vet ./...` to ensure clean Go code.

---

## Pull Request Process

1. Fork the repository on GitHub.
2. Create a feature branch (`git checkout -b feature/my-new-feature`).
3. Commit your changes with clear, concise commit messages.
4. Push to your fork (`git push origin feature/my-new-feature`).
5. Open a Pull Request against `main`.

---

## License

By contributing to `action-pin`, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).
