package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type mockResolver struct {
	sha   string
	err   error
	calls []string
}

func (r *mockResolver) Resolve(_ context.Context, owner, repo, ref string) (string, error) {
	r.calls = append(r.calls, owner+"/"+repo+"@"+ref)
	return r.sha, r.err
}

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

func TestCLI_ModeConflicts(t *testing.T) {
	for _, args := range [][]string{
		{"--check", "--fix"},
		{"--check", "--diff"},
		{"--fix", "--diff"},
		{"--check", "--fix", "--diff"},
	} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("expected code 1 on flag conflict, got %d", code)
			}
			if !strings.Contains(stderr.String(), "mutually exclusive") {
				t.Errorf("expected conflict message, got %s", stderr.String())
			}
		})
	}
}

type selectiveResolver struct {
	sha       string
	failingOn string
	calls     []string
}

func (r *selectiveResolver) Resolve(_ context.Context, owner, repo, ref string) (string, error) {
	call := owner + "/" + repo + "@" + ref
	r.calls = append(r.calls, call)
	if call == r.failingOn {
		return "", fmt.Errorf("network unavailable")
	}
	return r.sha, nil
}

func TestCLI_CheckMode_UnpinnedAndPinned(t *testing.T) {
	res := &mockResolver{sha: "b4ffde65f46336ab88eb53be808477a3936bae11"}
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
	code := runWithResolver([]string{"--check", "--file", workflowFile}, &stdout, &stderr, res)
	if code != 1 {
		t.Fatalf("expected exit code 1 for unpinned actions, got %d. stderr: %s, stdout: %s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "[UNPINNED]") {
		t.Errorf("expected [UNPINNED] in stdout, got: %s", stdout.String())
	}
	if len(res.calls) != 0 {
		t.Fatalf("check called resolver: %v", res.calls)
	}

	// 2. Fix it
	stdout.Reset()
	stderr.Reset()
	code = runWithResolver([]string{"--fix", "--file", workflowFile}, &stdout, &stderr, res)
	if code != 0 {
		t.Fatalf("expected exit code 0 for fix, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "[PINNED]") {
		t.Errorf("expected [PINNED] in stdout, got: %s", stdout.String())
	}
	if len(res.calls) != 1 || res.calls[0] != "actions/checkout@v4" {
		t.Fatalf("unexpected fix resolution calls: %v", res.calls)
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
	code = runWithResolver([]string{"--check", "--file", workflowFile}, &stdout, &stderr, res)
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

func TestCLI_OfflineChecks(t *testing.T) {
	const input = "jobs:\n  test:\n    steps:\n      - uses: nonexistent-owner/nonexistent-repo@missing-tag\n"
	for _, target := range []string{"--file", "--dir"} {
		for _, explicitCheck := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/check=%t", target, explicitCheck), func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "ci.yml")
				if err := os.WriteFile(path, []byte(input), 0644); err != nil {
					t.Fatal(err)
				}
				args := []string{target, path}
				if target == "--dir" {
					args[1] = dir
				}
				if explicitCheck {
					args = append(args, "--check")
				}
				res := &mockResolver{err: fmt.Errorf("network unavailable")}
				var stdout, stderr bytes.Buffer
				code := runWithResolver(args, &stdout, &stderr, res)
				if code != 1 || !strings.Contains(stdout.String(), "[UNPINNED]") || !strings.Contains(stdout.String(), "nonexistent-owner/nonexistent-repo@missing-tag") {
					t.Fatalf("check did not report unpinned ref: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
				}
				if len(res.calls) != 0 {
					t.Fatalf("offline check attempted resolution: %v", res.calls)
				}
				if strings.Contains(stdout.String(), "suggested:") || strings.Contains(stderr.String(), "network unavailable") {
					t.Fatalf("offline check emitted resolution output: stdout=%s stderr=%s", &stdout, &stderr)
				}
				output, err := os.ReadFile(path)
				if err != nil || string(output) != input {
					t.Fatalf("check changed source: %q, err=%v", output, err)
				}
			})
		}
	}
}

func TestCLI_Resolve(t *testing.T) {
	const sha = "b4ffde65f46336ab88eb53be808477a3936bae11"
	const input = "uses: actions/checkout@v4\n"
	for _, tc := range []struct {
		name   string
		flags  []string
		fix    bool
		resErr error
	}{
		{name: "default-check-with-resolution", flags: []string{"--resolve"}},
		{name: "explicit-check-with-resolution", flags: []string{"--check", "--resolve"}},
		{name: "fix-with-resolution", flags: []string{"--fix", "--resolve"}, fix: true},
		{name: "resolution-error", flags: []string{"--resolve"}, resErr: fmt.Errorf("network unavailable")},
	} {
		for _, target := range []string{"--file", "--dir"} {
			t.Run(tc.name+"/"+target, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "ci.yml")
				if err := os.WriteFile(path, []byte(input), 0644); err != nil {
					t.Fatal(err)
				}
				args := []string{target, path}
				if target == "--dir" {
					args[1] = dir
				}
				args = append(args, tc.flags...)
				res := &mockResolver{sha: sha, err: tc.resErr}
				var stdout, stderr bytes.Buffer
				code := runWithResolver(args, &stdout, &stderr, res)
				wantCode := 1
				if tc.fix {
					wantCode = 0
				}
				if code != wantCode || len(res.calls) != 1 || res.calls[0] != "actions/checkout@v4" {
					t.Fatalf("unexpected result: code=%d calls=%v stdout=%s stderr=%s", code, res.calls, &stdout, &stderr)
				}
				output, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if !tc.fix && string(output) != input {
					t.Fatal("resolved check changed source")
				}
				if tc.resErr != nil {
					if !strings.Contains(stderr.String(), "network unavailable") || strings.Contains(stdout.String(), "suggested:") {
						t.Fatalf("expected resolution error without suggestion: stdout=%s stderr=%s", &stdout, &stderr)
					}
				} else if tc.fix {
					if !strings.Contains(string(output), "actions/checkout@"+sha) || !strings.Contains(stdout.String(), "[PINNED]") {
						t.Fatalf("fix failed: output=%s stdout=%s", output, &stdout)
					}
				} else if !strings.Contains(stdout.String(), "(suggested: "+sha+")") {
					t.Fatalf("missing suggested SHA: %s", &stdout)
				}
			})
		}
	}
}

func TestCLI_DiffPreviewDoesNotWriteAndAllowsResolve(t *testing.T) {
	const sha = "b4ffde65f46336ab88eb53be808477a3936bae11"
	input := []byte("name: CI\r\njobs:\r\n  test:\r\n    steps:\r\n      - uses: actions/checkout@v4")
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow with space.yml")
	if err := os.WriteFile(path, input, 0o644); err != nil {
		t.Fatal(err)
	}

	res := &mockResolver{sha: sha}
	var stdout, stderr bytes.Buffer
	code := runWithResolver([]string{"--diff", "--resolve", "--file", path}, &stdout, &stderr, res)
	if code != 0 {
		t.Fatalf("expected successful preview, got %d; stderr: %s", code, stderr.String())
	}
	if len(res.calls) != 1 || res.calls[0] != "actions/checkout@v4" {
		t.Fatalf("unexpected resolver calls: %v", res.calls)
	}
	if stderr.Len() != 0 {
		t.Fatalf("successful diff wrote to stderr: %s", stderr.String())
	}
	patch := stdout.String()
	if !strings.HasPrefix(patch, "--- \"a/") || !strings.Contains(patch, "\n+++ \"b/") || !strings.Contains(patch, "\n@@ -") {
		t.Fatalf("stdout is not a unified patch: %q", patch)
	}
	if strings.Contains(patch, "[PINNED]") || strings.Contains(patch, "Success:") {
		t.Fatalf("stdout includes non-patch output: %s", patch)
	}
	if !strings.Contains(patch, "actions/checkout@"+sha) || !strings.Contains(patch, "\\ No newline at end of file") {
		t.Fatalf("preview omitted the expected edit or newline marker: %s", patch)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, input) {
		t.Fatalf("--diff changed source: got %q, want %q", got, input)
	}
}

func TestCLI_DiffDirectoryErrorDoesNotEmitPartialPatch(t *testing.T) {
	const sha = "b4ffde65f46336ab88eb53be808477a3936bae11"
	dir := t.TempDir()
	first := filepath.Join(dir, "a.yml")
	second := filepath.Join(dir, "b.yml")
	firstInput := []byte("uses: actions/checkout@v4\n")
	secondInput := []byte("uses: actions/setup-go@v5\n")
	if err := os.WriteFile(first, firstInput, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, secondInput, 0o644); err != nil {
		t.Fatal(err)
	}

	res := &selectiveResolver{sha: sha, failingOn: "actions/setup-go@v5"}
	var stdout, stderr bytes.Buffer
	code := runWithResolver([]string{"--diff", "--dir", dir}, &stdout, &stderr, res)
	if code != 1 {
		t.Fatalf("expected resolution error, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("error emitted a misleading partial patch: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "network unavailable") {
		t.Fatalf("missing resolution error: %s", stderr.String())
	}
	if len(res.calls) != 2 {
		t.Fatalf("expected both files to be planned before failure, calls=%v", res.calls)
	}
	for _, tc := range []struct {
		path string
		want []byte
	}{{first, firstInput}, {second, secondInput}} {
		got, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, tc.want) {
			t.Fatalf("preview changed %s: got %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestCLI_DiffPatchAppliesFromCurrentWorkingDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	const sha = "b4ffde65f46336ab88eb53be808477a3936bae11"
	repo := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	path := filepath.Join(repo, ".github", "workflows", "ci space.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	before := []byte("jobs:\r\n  test:\r\n    steps:\r\n      - uses: actions/checkout@v4")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "config", "core.autocrlf", "false").CombinedOutput(); err != nil {
		t.Fatalf("git config core.autocrlf: %v: %s", err, output)
	}

	res := &mockResolver{sha: sha}
	var stdout, stderr bytes.Buffer
	code := runWithResolver([]string{"--diff", "--file", path}, &stdout, &stderr, res)
	if code != 0 {
		t.Fatalf("expected successful preview, got %d: %s", code, stderr.String())
	}
	patch := stdout.Bytes()
	if !bytes.HasPrefix(patch, []byte("--- \"a/.github/workflows/ci space.yml\"\n")) {
		t.Fatalf("absolute input path was not made cwd-relative: %s", patch)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, before) {
		t.Fatalf("--diff changed file before applying patch: got=%q err=%v", got, err)
	}
	if err := os.WriteFile("preview.patch", patch, 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "apply", "--check", "preview.patch").CombinedOutput(); err != nil {
		t.Fatalf("git apply --check rejected preview: %v: %s\npatch:\n%s", err, output, patch)
	}
	if output, err := exec.Command("git", "apply", "preview.patch").CombinedOutput(); err != nil {
		t.Fatalf("git apply rejected preview: %v: %s", err, output)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after, []byte("actions/checkout@"+sha)) || bytes.HasSuffix(after, []byte("\n")) {
		t.Fatalf("applied preview did not preserve expected content: %q", after)
	}
}

func TestPatchPathResolvesWorkingDirectoryAliases(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "actual")
	if err := os.Mkdir(actual, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(actual, "workflow.yml")
	if err := os.WriteFile(file, []byte("name: CI\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(actual, alias); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(alias); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)

	if got, want := patchPath(file), "workflow.yml"; got != want {
		t.Fatalf("patchPath(%q) = %q, want %q", file, got, want)
	}
}
