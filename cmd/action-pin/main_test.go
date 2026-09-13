package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI_Version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "action-pin version") {
		t.Errorf("expected version string, got %s", stdout.String())
	}
}

func TestCLI_CheckAndFixConflict(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--check", "--fix"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected code 1 on flag conflict, got %d", code)
	}
	if !strings.Contains(stderr.String(), "cannot be used simultaneously") {
		t.Errorf("expected conflict message, got %s", stderr.String())
	}
}

func TestCLI_CheckMode_UnpinnedAndPinned(t *testing.T) {
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "ci.yml")

	// 1. Create unpinned workflow
	unpinnedContent := `name: CI
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
`
	if err := os.WriteFile(workflowFile, []byte(unpinnedContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--check", "--file", workflowFile}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 for unpinned actions, got %d. stderr: %s, stdout: %s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[UNPINNED]") {
		t.Errorf("expected [UNPINNED] in stdout, got: %s", stdout.String())
	}

	// 2. Fix it
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--fix", "--file", workflowFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 for fix, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "[PINNED]") {
		t.Errorf("expected [PINNED] in stdout, got: %s", stdout.String())
	}

	// Verify file content was updated
	updatedBytes, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updatedBytes), "[pinned by action-pin]") {
		t.Errorf("expected pinned comment in file, got:\n%s", string(updatedBytes))
	}

	// 3. Re-run check on now-pinned workflow
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"--check", "--file", workflowFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 after pinning, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already pinned") {
		t.Errorf("expected 'already pinned' message, got: %s", stdout.String())
	}
}

func TestCLI_DirMode(t *testing.T) {
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "ci.yml")

	// Pinned action
	pinnedContent := `name: CI
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4 [pinned by action-pin]
`
	if err := os.WriteFile(workflowFile, []byte(pinnedContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir", tempDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 for pinned directory, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "All actions are pinned") {
		t.Errorf("expected success message, got: %s", stdout.String())
	}
}

func TestCLI_NonExistentDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir", "non/existent/dir/path"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected code 1 on non-existent dir, got %d", code)
	}
}

func TestCLI_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0 for --help, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Errorf("expected usage message in stderr, got: %s", stderr.String())
	}
}

func TestCLI_DefaultCheckMode(t *testing.T) {
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "ci.yml")

	// Pinned workflow
	pinnedContent := `name: CI
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4 [pinned by action-pin]
`
	if err := os.WriteFile(workflowFile, []byte(pinnedContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	// Running with only --file (no --check, no --fix) should default to check mode
	code := run([]string{"--file", workflowFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already pinned") {
		t.Errorf("expected 'already pinned' in stdout, got: %s", stdout.String())
	}
}

func TestCLI_NonExistentFile(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--file", "non/existent/file.yml"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected code 1 for missing file, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Error processing") {
		t.Errorf("expected error message in stderr, got: %s", stderr.String())
	}
}

func TestCLI_DefaultDirNotExist(t *testing.T) {
	// Temporarily switch to an empty temp directory without .github/workflows
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	var stdout, stderr bytes.Buffer
	// Default invocation with no flags in directory without .github/workflows should exit 0
	code := run([]string{}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0 when default dir does not exist, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "does not exist. Nothing to pin.") {
		t.Errorf("expected graceful notice, got: %s", stdout.String())
	}
}

func TestCLI_Verbose(t *testing.T) {
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "ci.yml")
	pinnedContent := `name: CI
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11 # v4 [pinned by action-pin]
`
	if err := os.WriteFile(workflowFile, []byte(pinnedContent), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--dir", tempDir, "--verbose"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
}

func TestCLI_DynamicExpression(t *testing.T) {
	tempDir := t.TempDir()
	workflowFile := filepath.Join(tempDir, "ci.yml")
	content := `name: Dynamic
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@${{ matrix.version }}
`
	if err := os.WriteFile(workflowFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	// Dynamic action should be ignored and not crash
	code := run([]string{"--file", workflowFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0 for dynamic action workflow, got %d. stderr: %s", code, stderr.String())
	}
}

