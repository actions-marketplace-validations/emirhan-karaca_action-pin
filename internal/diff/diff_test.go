package diff

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnified_NoChange(t *testing.T) {
	if got := Unified("workflow.yml", []byte("same\n"), []byte("same\n")); got != nil {
		t.Fatalf("expected no patch, got %q", got)
	}
}

func TestUnified_ContextAndNoFinalNewline(t *testing.T) {
	before := []byte("one\ntwo\nold\nfour\nfive\nsix")
	after := []byte("one\ntwo\nnew\nfour\nfive\nsix")
	got := string(Unified("workflow.yml", before, after))
	want := "--- a/workflow.yml\n" +
		"+++ b/workflow.yml\n" +
		"@@ -1,6 +1,6 @@\n" +
		" one\n" +
		" two\n" +
		"-old\n" +
		"+new\n" +
		" four\n" +
		" five\n" +
		" six\n" +
		"\\ No newline at end of file\n"
	if got != want {
		t.Fatalf("unexpected patch:\n%s", got)
	}
}

func TestUnified_EmptyAndCRLF(t *testing.T) {
	t.Run("empty file", func(t *testing.T) {
		got := string(Unified("empty.yml", nil, []byte("created\n")))
		if !strings.Contains(got, "@@ -0,0 +1,1 @@\n+created\n") {
			t.Fatalf("empty-file patch has an invalid hunk: %q", got)
		}
	})

	t.Run("crlf content is preserved", func(t *testing.T) {
		got := Unified("workflow.yml", []byte("one\r\ntwo\r\n"), []byte("one\r\nthree\r\n"))
		if !bytes.Contains(got, []byte("-two\r\n+three\r\n")) {
			t.Fatalf("CRLF source lines were not preserved: %q", got)
		}
	})
}

func TestQuotePath(t *testing.T) {
	if got, want := QuotePath("a/space name\tand\\backslash"), `"a/space name\tand\\backslash"`; got != want {
		t.Fatalf("QuotePath() = %q, want %q", got, want)
	}
	if got := QuotePath("a/plain.yml"); got != "a/plain.yml" {
		t.Fatalf("plain path was unexpectedly quoted: %q", got)
	}
}

func TestUnified_GitApply(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	for _, tc := range []struct {
		name   string
		path   string
		before []byte
		after  []byte
	}{
		{
			name:   "lf",
			path:   "workflows/ci.yml",
			before: []byte("one\ntwo\nthree\nfour\n"),
			after:  []byte("one\nupdated\nthree\nfour\n"),
		},
		{
			name:   "crlf",
			path:   "workflows/crlf.yml",
			before: []byte("one\r\ntwo\r\nthree\r\n"),
			after:  []byte("one\r\nupdated\r\nthree\r\n"),
		},
		{
			name:   "no final newline",
			path:   "workflows/no-newline.yml",
			before: []byte("one\ntwo"),
			after:  []byte("one\nupdated"),
		},
		{
			name:   "add final newline",
			path:   "workflows/add-newline.yml",
			before: []byte("one\ntwo"),
			after:  []byte("one\ntwo\n"),
		},
		{
			name:   "insert into empty file",
			path:   "workflows/empty.yml",
			before: nil,
			after:  []byte("created\n"),
		},
		{
			name:   "delete to empty file",
			path:   "workflows/deleted.yml",
			before: []byte("removed\n"),
			after:  nil,
		},
		{
			name:   "quoted path",
			path:   "workflows/space name file.yml",
			before: []byte("one\nold\n"),
			after:  []byte("one\nnew\n"),
		},
		{
			name:   "unicode path",
			path:   "workflows/café.yml",
			before: []byte("one\nold\n"),
			after:  []byte("one\nnew\n"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			filePath := filepath.Join(repo, filepath.FromSlash(tc.path))
			if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filePath, tc.before, 0o644); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			if output, err := exec.Command("git", "-C", repo, "config", "core.autocrlf", "false").CombinedOutput(); err != nil {
				t.Fatalf("git config core.autocrlf: %v: %s", err, output)
			}

			patchPath := filepath.Join(repo, "change.patch")
			if err := os.WriteFile(patchPath, Unified(tc.path, tc.before, tc.after), 0o644); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("git", "-C", repo, "apply", "--check", "change.patch").CombinedOutput(); err != nil {
				t.Fatalf("git apply --check rejected patch: %v: %s\npatch:\n%s", err, output, Unified(tc.path, tc.before, tc.after))
			}
			if output, err := exec.Command("git", "-C", repo, "apply", "change.patch").CombinedOutput(); err != nil {
				t.Fatalf("git apply rejected patch: %v: %s", err, output)
			}
			got, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.after) {
				t.Fatalf("applied content = %q, want %q", got, tc.after)
			}
		})
	}
}
