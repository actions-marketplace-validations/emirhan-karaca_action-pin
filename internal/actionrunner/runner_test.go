package actionrunner_test

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Exercise the actual composite-action entry point with local tools and release
// archives. No command in these tests needs access to GitHub or a Go toolchain.
const fakeBinary = `#!/usr/bin/env bash
printf 'executed\nbinary-cwd=%s\n' "$(pwd -P)" >> "$TEST_LOG"
printf 'binary-arg=%s\n' "$@" >> "$TEST_LOG"
if [[ -n "${TEST_BINARY_STDOUT:-}" ]]; then
  printf '%s' "$TEST_BINARY_STDOUT"
fi
exit "${TEST_BINARY_EXIT:-0}"
`

const fakeCurl = `#!/usr/bin/env bash
printf 'downloaded\n' >> "$TEST_LOG"
printf 'curl-arg=%s\n' "$@" >> "$TEST_LOG"
if [[ "${TEST_CURL_EXIT:-0}" != 0 ]]; then exit "$TEST_CURL_EXIT"; fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) output=$2; shift 2 ;;
    *) shift ;;
  esac
done
cp "$TEST_ARCHIVE" "$output"
`

const fakeGo = `#!/usr/bin/env bash
printf 'built\ngo-cwd=%s\n' "$(pwd -P)" >> "$TEST_LOG"
printf 'go-arg=%s\n' "$@" >> "$TEST_LOG"
printf 'go-env=%s|%s|%s|%s\n' "$GOWORK" "$GOFLAGS" "$GOTOOLCHAIN" "$CGO_ENABLED" >> "$TEST_LOG"
printf 'go-module=%s\n' "$GO111MODULE" >> "$TEST_LOG"
printf 'go-target=%s|%s|%s|%s|%s|%s|%s\n' "$GOENV" "$GOOS" "$GOARCH" "$GOAMD64" "$GOARM" "$GOARM64" "$GOEXPERIMENT" >> "$TEST_LOG"
if [[ "${TEST_GO_EXIT:-0}" != 0 ]]; then exit "$TEST_GO_EXIT"; fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) output=$2; shift 2 ;;
    *) shift ;;
  esac
done
cp "$TEST_BINARY" "$output"
chmod +x "$output"
`

type runner struct {
	bash     string
	script   string
	bin      string
	caller   string
	source   string
	log      string
	archive  string
	checksum string
	binary   string
}

func newRunner(t *testing.T, archiveOS string) *runner {
	t.Helper()
	root := filepath.Join(t.TempDir(), "fixture with spaces")
	r := &runner{
		bash:    findBash(t),
		bin:     filepath.Join(root, "mock commands"),
		caller:  filepath.Join(root, "caller repository"),
		source:  filepath.Join(root, "selected action source"),
		log:     filepath.Join(root, "execution.log"),
		binary:  filepath.Join(root, "fixture binary"),
		archive: filepath.Join(root, "release archive"),
	}
	var err error
	r.script, err = filepath.Abs(filepath.Join("..", "..", "scripts", "run-action.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{r.bin, r.caller, r.source} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, r.binary, fakeBinary)
	writeFile(t, filepath.Join(r.source, "go.mod"), "module test.invalid/selected-action\n\ngo 1.22\n")
	writeFile(t, filepath.Join(r.bin, "curl"), fakeCurl)
	writeFile(t, filepath.Join(r.bin, "go"), fakeGo)
	writeFile(t, filepath.Join(r.bin, "uname"), `#!/usr/bin/env bash
case "$1" in
  -s) printf '%s\n' "$TEST_OS" ;;
  -m) printf '%s\n' "$TEST_ARCH" ;;
  *) exit 1 ;;
esac
`)
	writeFile(t, filepath.Join(r.bin, "action-pin"), "#!/usr/bin/env bash\nprintf 'path-binary-executed\\n' >> \"$TEST_LOG\"\nexit 90\n")
	r.checksum = makeArchive(t, r.archive, archiveOS)
	return r
}

func findBash(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		// Windows' bash.exe on PATH can be a WSL launcher, which cannot run
		// these Git Bash fixtures. GitHub's Windows runners install Git here.
		for _, dir := range []string{os.Getenv("ProgramFiles"), `C:\Program Files`} {
			path := filepath.Join(dir, "Git", "bin", "bash.exe")
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
		t.Skip("runner integration tests require Git Bash on Windows")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("runner integration tests require bash")
	}
	return bash
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func makeArchive(t *testing.T, path, archiveOS string) string {
	t.Helper()
	var data strings.Builder
	if archiveOS == "windows" {
		zw := zip.NewWriter(&data)
		header := &zip.FileHeader{Name: "action-pin.exe", Method: zip.Deflate}
		header.SetMode(0755)
		entry, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(fakeBinary)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(&data)
		tw := tar.NewWriter(gz)
		if err := tw.WriteHeader(&tar.Header{Name: "action-pin", Mode: 0755, Size: int64(len(fakeBinary))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(fakeBinary)); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	bytes := []byte(data.String())
	if err := os.WriteFile(path, bytes, 0644); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(bytes))
}

func shellPath(path string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" && len(path) > 2 && path[1] == ':' {
		return "/" + strings.ToLower(path[:1]) + path[2:]
	}
	return path
}

func (r *runner) run(t *testing.T, overrides map[string]string) (int, string, string) {
	t.Helper()
	code, stdout, stderr, log := r.runStreams(t, overrides)
	return code, stdout + stderr, log
}

func (r *runner) runStreams(t *testing.T, overrides map[string]string) (int, string, string, string) {
	t.Helper()
	env := map[string]string{
		"ACTION_PATH":        shellPath(r.source),
		"INPUT_VERSION":      "v1.2.3",
		"INPUT_CHECKSUM":     r.checksum,
		"INPUT_CHECK":        "true",
		"INPUT_FIX":          "false",
		"INPUT_DIFF":         "false",
		"INPUT_RESOLVE":      "false",
		"INPUT_DIR":          "",
		"TEST_OS":            "Linux",
		"TEST_ARCH":          "x86_64",
		"TEST_BIN":           shellPath(r.bin),
		"TEST_SCRIPT":        shellPath(r.script),
		"TEST_LOG":           shellPath(r.log),
		"TEST_ARCHIVE":       shellPath(r.archive),
		"TEST_BINARY":        shellPath(r.binary),
		"TEST_BINARY_EXIT":   "0",
		"TEST_BINARY_STDOUT": "",
		"TEST_CURL_EXIT":     "0",
		"TEST_GO_EXIT":       "0",
	}
	for key, value := range overrides {
		env[key] = value
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bash, "--noprofile", "--norc", "-c", `export PATH="$TEST_BIN:$PATH"; exec bash "$TEST_SCRIPT"`)
	cmd.Dir = r.caller
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := env[strings.ToUpper(key)]; !replaced && !strings.EqualFold(key, "BASH_ENV") && !strings.EqualFold(key, "ENV") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("runner timed out: %v\nstdout:\n%s\nstderr:\n%s", ctx.Err(), stdout.String(), stderr.String())
	}
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("start runner: %v", err)
		}
	}
	log, err := os.ReadFile(r.log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return code, stdout.String(), stderr.String(), string(log)
}

func assertContains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("missing %q in:\n%s", want, text)
	}
}

func assertNotExecuted(t *testing.T, log string) {
	t.Helper()
	if strings.Contains(log, "executed") {
		t.Fatalf("runner executed a binary unexpectedly:\n%s", log)
	}
}

func assertWorkingDirectory(t *testing.T, log, key, want string) {
	t.Helper()
	for _, line := range strings.Split(log, "\n") {
		if !strings.HasPrefix(line, key+"=") {
			continue
		}
		got := strings.TrimPrefix(line, key+"=")
		if runtime.GOOS == "windows" && len(got) > 2 && got[0] == '/' && got[2] == '/' {
			got = got[1:2] + ":" + got[2:]
		}
		actual, err := os.Stat(filepath.FromSlash(got))
		if err != nil {
			t.Fatalf("read reported working directory %q: %v", got, err)
		}
		expected, err := os.Stat(want)
		if err != nil {
			t.Fatal(err)
		}
		// pwd resolves symlinks and Windows short names, so compare directory
		// identity instead of their textual paths (/var vs /private/var, etc.).
		if !os.SameFile(actual, expected) {
			t.Errorf("%s = %q, want directory %q", key, got, want)
		}
		return
	}
	t.Errorf("missing %s in:\n%s", key, log)
}

func TestReleaseVerificationAndExecution(t *testing.T) {
	for _, tc := range []struct{ name, os, arch, asset string }{
		{"linux", "Linux", "x86_64", "linux_amd64.tar.gz"},
		{"darwin", "Darwin", "arm64", "darwin_arm64.tar.gz"},
		{"windows", "MINGW64_NT-10.0", "x86_64", "windows_amd64.zip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunner(t, tc.name)
			code, output, log := r.run(t, map[string]string{
				"TEST_OS": tc.os, "TEST_ARCH": tc.arch,
				"INPUT_CHECKSUM": strings.ToUpper(r.checksum),
				"INPUT_RESOLVE":  "true", "INPUT_DIR": "workflows with spaces",
			})
			if code != 0 {
				t.Fatalf("exit %d:\n%s\n%s", code, output, log)
			}
			assertContains(t, log, "curl-arg=https://github.com/emirhan-karaca/action-pin/releases/download/v1.2.3/action-pin_1.2.3_"+tc.asset+"\n")
			assertContains(t, log, "binary-arg=--check\nbinary-arg=--resolve\nbinary-arg=--dir\nbinary-arg=workflows with spaces\n")
			assertWorkingDirectory(t, log, "binary-cwd", r.caller)
			if strings.Contains(log, "path-binary-executed") || strings.Contains(log, "built\n") {
				t.Fatalf("release mode used an unexpected binary or build:\n%s", log)
			}
		})
	}
}

func TestRunnerRejectsUnsafeInputs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides map[string]string
		message   string
		download  bool
	}{
		{"mismatched-digest", map[string]string{"INPUT_CHECKSUM": strings.Repeat("0", 64)}, "SHA-256 mismatch", true},
		{"missing-digest", map[string]string{"INPUT_CHECKSUM": ""}, "requires the 64-character SHA-256 checksum", false},
		{"latest", map[string]string{"INPUT_VERSION": "latest"}, "version must be source or an exact release tag", false},
		{"source-with-checksum", map[string]string{"INPUT_VERSION": "source"}, "checksum is only valid with an exact release version", false},
		{"invalid-diff", map[string]string{"INPUT_DIFF": "yes"}, "check, fix, diff, and resolve must be true or false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunner(t, "linux")
			code, output, log := r.run(t, tc.overrides)
			if code == 0 {
				t.Fatalf("invalid release settings succeeded:\n%s", output)
			}
			assertContains(t, output, tc.message)
			assertNotExecuted(t, log)
			if downloaded := strings.Contains(log, "downloaded\n"); downloaded != tc.download {
				t.Fatalf("downloaded=%t, want %t:\n%s", downloaded, tc.download, log)
			}
		})
	}
}

func TestDownloadFailureDoesNotExecute(t *testing.T) {
	r := newRunner(t, "linux")
	code, output, log := r.run(t, map[string]string{"TEST_CURL_EXIT": "22"})
	if code != 22 {
		t.Fatalf("exit %d, want curl exit 22:\n%s", code, output)
	}
	assertContains(t, log, "downloaded\n")
	assertNotExecuted(t, log)
}

func TestSourceBuildUsesSelectedAction(t *testing.T) {
	r := newRunner(t, "linux")
	code, output, log := r.run(t, map[string]string{
		"INPUT_VERSION": "source", "INPUT_CHECKSUM": "", "INPUT_FIX": "true",
		"INPUT_DIR": "workflows with spaces", "GOWORK": "caller.go.work",
		"GOFLAGS": "-race", "GOTOOLCHAIN": "auto", "CGO_ENABLED": "1",
		"GOENV": "caller-go-env", "GOOS": "linux", "GOARCH": "arm64",
		"GO111MODULE": "off",
		"GOAMD64":     "v3", "GOARM": "7", "GOARM64": "v9.0", "GOEXPERIMENT": "invalid-caller-experiment",
	})
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, output, log)
	}
	assertWorkingDirectory(t, log, "go-cwd", r.source)
	assertContains(t, log, "go-env=off||local|0\n")
	assertContains(t, log, "go-module=on\n")
	assertContains(t, log, "go-target=off||||||\n")
	assertContains(t, log, "go-arg=build\ngo-arg=-mod=readonly\ngo-arg=-trimpath\ngo-arg=-o\n")
	assertContains(t, log, "go-arg=./cmd/action-pin\n")
	assertWorkingDirectory(t, log, "binary-cwd", r.caller)
	assertContains(t, log, "binary-arg=--fix\nbinary-arg=--dir\nbinary-arg=workflows with spaces\n")
	if strings.Contains(log, "path-binary-executed") || strings.Contains(log, "downloaded\n") {
		t.Fatalf("source mode used PATH binary or downloaded a release:\n%s", log)
	}
}

func TestDiffModeForwardsDiffInsteadOfDefaultCheck(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides map[string]string
	}{
		{
			name: "release",
			overrides: map[string]string{
				"INPUT_DIFF": "true", "INPUT_DIR": "workflows with spaces",
			},
		},
		{
			name: "source",
			overrides: map[string]string{
				"INPUT_VERSION": "source", "INPUT_CHECKSUM": "", "INPUT_DIFF": "true", "INPUT_DIR": "workflows with spaces",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunner(t, "linux")
			code, output, log := r.run(t, tc.overrides)
			if code != 0 {
				t.Fatalf("exit %d:\n%s\n%s", code, output, log)
			}
			assertContains(t, log, "binary-arg=--diff\nbinary-arg=--dir\nbinary-arg=workflows with spaces\n")
			for _, unexpected := range []string{"binary-arg=--check\n", "binary-arg=--fix\n"} {
				if strings.Contains(log, unexpected) {
					t.Fatalf("diff mode forwarded %q unexpectedly:\n%s", strings.TrimSpace(unexpected), log)
				}
			}
		})
	}
}

func TestDiffModeKeepsLauncherOutputOffStdout(t *testing.T) {
	patch := "--- a/.github/workflows/ci.yml\n+++ b/.github/workflows/ci.yml\n"
	for _, tc := range []struct {
		name      string
		overrides map[string]string
		notice    string
	}{
		{
			name:   "release",
			notice: "Downloading action-pin v1.2.3 for linux/amd64\n",
		},
		{
			name: "source",
			overrides: map[string]string{
				"INPUT_VERSION": "source", "INPUT_CHECKSUM": "",
			},
			notice: "Building action-pin from the selected action source\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunner(t, "linux")
			overrides := map[string]string{
				"INPUT_DIFF":         "true",
				"TEST_BINARY_STDOUT": patch,
			}
			for key, value := range tc.overrides {
				overrides[key] = value
			}
			code, stdout, stderr, log := r.runStreams(t, overrides)
			if code != 0 {
				t.Fatalf("exit %d:\nstdout:\n%s\nstderr:\n%s\n%s", code, stdout, stderr, log)
			}
			if stdout != patch {
				t.Fatalf("stdout = %q, want only the binary patch %q", stdout, patch)
			}
			assertContains(t, stderr, tc.notice)
		})
	}
}

func TestDiffModeAllowsRedundantResolve(t *testing.T) {
	r := newRunner(t, "linux")
	code, output, log := r.run(t, map[string]string{
		"INPUT_DIFF": "true", "INPUT_RESOLVE": "true",
	})
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, output, log)
	}
	assertContains(t, log, "binary-arg=--diff\nbinary-arg=--resolve\n")
}

func TestCheckFalseStillRunsCheck(t *testing.T) {
	r := newRunner(t, "linux")
	code, output, log := r.run(t, map[string]string{"INPUT_CHECK": "false"})
	if code != 0 {
		t.Fatalf("exit %d:\n%s\n%s", code, output, log)
	}
	assertContains(t, log, "binary-arg=--check\n")
	for _, unexpected := range []string{"binary-arg=--fix\n", "binary-arg=--diff\n"} {
		if strings.Contains(log, unexpected) {
			t.Fatalf("check=false forwarded %q unexpectedly:\n%s", strings.TrimSpace(unexpected), log)
		}
	}
}

func TestRunnerRejectsFixAndDiffBeforeBuildOrDownload(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides map[string]string
	}{
		{
			name:      "release",
			overrides: map[string]string{"INPUT_FIX": "true", "INPUT_DIFF": "true"},
		},
		{
			name: "source",
			overrides: map[string]string{
				"INPUT_VERSION": "source", "INPUT_CHECKSUM": "", "INPUT_FIX": "true", "INPUT_DIFF": "true",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRunner(t, "linux")
			code, output, log := r.run(t, tc.overrides)
			if code == 0 {
				t.Fatalf("fix and diff unexpectedly succeeded:\n%s", output)
			}
			assertContains(t, output, "fix and diff cannot both be true")
			assertNotExecuted(t, log)
			if strings.Contains(log, "built\n") || strings.Contains(log, "downloaded\n") {
				t.Fatalf("invalid mode reached a build or download:\n%s", log)
			}
		})
	}
}

func TestSourceBuildFailureDoesNotFallBack(t *testing.T) {
	r := newRunner(t, "linux")
	code, output, log := r.run(t, map[string]string{"INPUT_VERSION": "source", "INPUT_CHECKSUM": "", "TEST_GO_EXIT": "19"})
	if code != 19 {
		t.Fatalf("exit %d, want build exit 19:\n%s", code, output)
	}
	assertContains(t, log, "built\n")
	assertNotExecuted(t, log)
	if strings.Contains(log, "downloaded\n") {
		t.Fatalf("failed source build fell back to a release:\n%s", log)
	}
}

func TestBinaryExitCodePreserved(t *testing.T) {
	for _, source := range []bool{false, true} {
		t.Run(fmt.Sprintf("source=%t", source), func(t *testing.T) {
			r := newRunner(t, "linux")
			overrides := map[string]string{"TEST_BINARY_EXIT": "7"}
			if source {
				overrides["INPUT_VERSION"] = "source"
				overrides["INPUT_CHECKSUM"] = ""
			}
			code, output, log := r.run(t, overrides)
			if code != 7 {
				t.Fatalf("exit %d, want binary exit 7:\n%s\n%s", code, output, log)
			}
			assertContains(t, log, "executed\n")
		})
	}
}
