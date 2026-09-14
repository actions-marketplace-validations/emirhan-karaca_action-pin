# Changelog

## [2.0.0] - 2026-09-14

### Breaking changes

- The Go module is now `github.com/emirhan-karaca/action-pin/v2`. Install the CLI with `go install github.com/emirhan-karaca/action-pin/v2/cmd/action-pin@v2.0.0`.
- The composite Action now defaults to `version: source`, builds the selected Action checkout, and requires Go 1.22 or newer on the runner.
- Release-binary mode accepts only an exact release tag and requires the matching 64-character SHA-256 archive checksum. The former `latest` behavior is no longer supported.

### Added

- Default CLI checks are offline and report unpinned references without resolving them. Use `--resolve` to request SHA suggestions.
- `--diff` and the Action `diff` input produce a reviewable unified patch without changing workflow files.
- Fix and diff modes prepare the selected workflow set before writes, reject unsafe symlink targets, preserve file permissions, and report partial replacement failures.

### Compatibility

- v1.0.x keeps its previous Action launcher behavior: it selects `latest` by default, has no source-mode/checksum contract, resolves CLI checks online, and does not provide `--resolve` or `--diff`.

### Validation and release assets

- The release candidate passed seven CI jobs: Linux, macOS, and Windows on Go 1.23.x and 1.24.x, plus the workflow self-check.
- Publishing `v2.0.0` is expected to produce six archives for Linux, macOS, and Windows on amd64 and arm64, plus `checksums.txt`. Copy platform-specific SHA-256 values from that manifest; no archive digest is recorded here before publication.
