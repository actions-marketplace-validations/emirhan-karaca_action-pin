package pinner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const planTestSHA = "b4ffde65f46336ab88eb53be808477a3936bae11"

type planTestResolver struct {
	mapping map[string]string
}

func (r planTestResolver) Resolve(ctx context.Context, owner, repo, ref string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key := fmt.Sprintf("%s/%s@%s", owner, repo, ref)
	sha, ok := r.mapping[key]
	if !ok {
		return "", fmt.Errorf("ref not found: %s", key)
	}
	return sha, nil
}

func newPlanTestPinner() *Pinner {
	return New(planTestResolver{mapping: map[string]string{
		"actions/checkout@v4": planTestSHA,
	}})
}

func writePlanTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func readPlanTestFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertNoStagedFiles(t *testing.T, dir string) {
	t.Helper()
	staged, err := filepath.Glob(filepath.Join(dir, ".action-pin-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 0 {
		t.Fatalf("staged files were not cleaned up: %v", staged)
	}
}

func TestPlanDirectoryHasNoSideEffectsUntilApplyAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ci.yml")
	original := "uses: actions/checkout@v4\n"
	writePlanTestFile(t, path, original, 0640)
	beforeInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := newPlanTestPinner().PlanDirectory(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Result.FilesChecked != 1 || plan.Result.FilesModified != 0 || plan.Result.UnpinnedCount != 1 {
		t.Fatalf("unexpected planned result: %+v", plan.Result)
	}
	if len(plan.Result.Findings) != 1 || plan.Result.Findings[0].ResolvedSHA != planTestSHA {
		t.Fatalf("plan did not resolve the finding: %+v", plan.Result.Findings)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != path {
		t.Fatalf("unexpected changes: %+v", plan.Changes)
	}
	if !bytes.Equal(plan.Changes[0].Before, []byte(original)) || !strings.Contains(string(plan.Changes[0].After), planTestSHA) {
		t.Fatalf("unexpected change contents: %+v", plan.Changes[0])
	}
	if got := readPlanTestFile(t, path); !bytes.Equal(got, []byte(original)) {
		t.Fatalf("planning changed source: %q", got)
	}

	if err := plan.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if plan.Result.FilesModified != 1 {
		t.Fatalf("FilesModified = %d, want 1", plan.Result.FilesModified)
	}
	if got := readPlanTestFile(t, path); !strings.Contains(string(got), "actions/checkout@"+planTestSHA) {
		t.Fatalf("apply did not write the planned content: %q", got)
	}
	afterInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if afterInfo.Mode().Perm() != beforeInfo.Mode().Perm() {
		t.Fatalf("mode changed from %o to %o", beforeInfo.Mode().Perm(), afterInfo.Mode().Perm())
	}
}

func TestProcessDirectoryFixDoesNotWriteWhenPlanningFailsLate(t *testing.T) {
	for _, tc := range []struct {
		name string
		late string
	}{
		{name: "invalid-yaml", late: "jobs: [unclosed list"},
		{name: "unresolvable-ref", late: "uses: example/missing@v1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			earlyPath := filepath.Join(dir, "a.yml")
			latePath := filepath.Join(dir, "z.yml")
			early := "uses: actions/checkout@v4\n"
			writePlanTestFile(t, earlyPath, early, 0644)
			writePlanTestFile(t, latePath, tc.late, 0644)

			result, err := newPlanTestPinner().ProcessDirectory(context.Background(), dir, true)
			if err == nil {
				t.Fatal("expected planning error")
			}
			if result == nil || result.FilesModified != 0 {
				t.Fatalf("unexpected result after planning failure: %+v", result)
			}
			if got := readPlanTestFile(t, earlyPath); !bytes.Equal(got, []byte(early)) {
				t.Fatalf("late planning failure changed earlier file: %q", got)
			}
			assertNoStagedFiles(t, dir)

			plan, planErr := newPlanTestPinner().PlanDirectory(context.Background(), dir)
			if planErr == nil {
				t.Fatal("expected planning error")
			}
			if err := plan.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "incomplete") {
				t.Fatalf("partial plan was unexpectedly applicable: %v", err)
			}
		})
	}
}

func TestPlanApplyRejectsStaleSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ci.yml")
	writePlanTestFile(t, path, "uses: actions/checkout@v4\n", 0644)

	plan, err := newPlanTestPinner().PlanFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	stale := "uses: actions/checkout@v5\n"
	writePlanTestFile(t, path, stale, 0644)

	err = plan.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "content changed") {
		t.Fatalf("expected stale-source error, got %v", err)
	}
	if plan.Result.FilesModified != 0 {
		t.Fatalf("stale apply reported modifications: %+v", plan.Result)
	}
	if got := readPlanTestFile(t, path); !bytes.Equal(got, []byte(stale)) {
		t.Fatalf("stale apply overwrote source: %q", got)
	}
	assertNoStagedFiles(t, dir)
}

func TestPlanApplyCleansStagedReadOnlyFilesOnStagingFailure(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "a.yml")
	secondPath := filepath.Join(dir, "b.yml")
	first := "uses: actions/checkout@v4\n"
	second := "uses: actions/checkout@v4\n"
	writePlanTestFile(t, firstPath, first, 0400)
	writePlanTestFile(t, secondPath, second, 0400)

	plan, err := newPlanTestPinner().PlanDirectory(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	originalStage := stageChangeFile
	calls := 0
	stageChangeFile = func(change Change) (string, error) {
		calls++
		if calls == 2 {
			return "", errors.New("forced staging failure")
		}
		return stageChange(change)
	}
	t.Cleanup(func() { stageChangeFile = originalStage })

	err = plan.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "forced staging failure") {
		t.Fatalf("expected staging failure, got %v", err)
	}
	if plan.Result.FilesModified != 0 {
		t.Fatalf("staging failure reported modifications: %+v", plan.Result)
	}
	if got := readPlanTestFile(t, firstPath); !bytes.Equal(got, []byte(first)) {
		t.Fatalf("staging failure changed first file: %q", got)
	}
	if got := readPlanTestFile(t, secondPath); !bytes.Equal(got, []byte(second)) {
		t.Fatalf("staging failure changed second file: %q", got)
	}
	assertNoStagedFiles(t, dir)
}

func TestPlanApplyReportsPartialReplacement(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "a.yml")
	secondPath := filepath.Join(dir, "b.yml")
	first := "uses: actions/checkout@v4\n"
	second := "uses: actions/checkout@v4\n"
	writePlanTestFile(t, firstPath, first, 0644)
	writePlanTestFile(t, secondPath, second, 0644)

	plan, err := newPlanTestPinner().PlanDirectory(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	originalReplace := replaceStagedFile
	calls := 0
	replaceStagedFile = func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return errors.New("forced replacement failure")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { replaceStagedFile = originalReplace })

	err = plan.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "after 1 file") {
		t.Fatalf("expected partial-application error, got %v", err)
	}
	if plan.Result.FilesModified != 1 {
		t.Fatalf("FilesModified = %d, want 1", plan.Result.FilesModified)
	}
	if got := readPlanTestFile(t, firstPath); !strings.Contains(string(got), planTestSHA) {
		t.Fatalf("first file was not applied: %q", got)
	}
	if got := readPlanTestFile(t, secondPath); !bytes.Equal(got, []byte(second)) {
		t.Fatalf("second file changed after replacement failure: %q", got)
	}
	assertNoStagedFiles(t, dir)
}

func TestPlanRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.yml")
	link := filepath.Join(dir, "linked.yml")
	original := "uses: actions/checkout@v4\n"
	writePlanTestFile(t, target, original, 0644)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	if _, err := newPlanTestPinner().PlanFile(context.Background(), link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink planning error, got %v", err)
	}
	if _, _, err := newPlanTestPinner().ProcessFile(context.Background(), link, true); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink fix error, got %v", err)
	}
	if got := readPlanTestFile(t, target); !bytes.Equal(got, []byte(original)) {
		t.Fatalf("symlink target was changed: %q", got)
	}
}

func TestPlanDirectoryRejectsSymlinkRootWithTrailingSeparator(t *testing.T) {
	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "ci.yml")
	original := "uses: actions/checkout@v4\n"
	writePlanTestFile(t, targetPath, original, 0644)

	link := filepath.Join(t.TempDir(), "workflows")
	if err := os.Symlink(targetDir, link); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	for _, root := range []string{link, link + string(os.PathSeparator)} {
		t.Run(root, func(t *testing.T) {
			plan, err := newPlanTestPinner().PlanDirectory(context.Background(), root)
			if err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("expected symlink root error, got plan=%+v err=%v", plan, err)
			}
			if plan.Result.FilesChecked != 0 || len(plan.Changes) != 0 {
				t.Fatalf("symlink root was inspected: %+v", plan)
			}
			if got := readPlanTestFile(t, targetPath); !bytes.Equal(got, []byte(original)) {
				t.Fatalf("symlink root changed target: %q", got)
			}
		})
	}
}

func TestPlanApplyHonorsCanceledContextBeforeWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ci.yml")
	original := "uses: actions/checkout@v4\n"
	writePlanTestFile(t, path, original, 0644)
	plan, err := newPlanTestPinner().PlanFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := plan.Apply(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply error = %v, want context cancellation", err)
	}
	if got := readPlanTestFile(t, path); !bytes.Equal(got, []byte(original)) {
		t.Fatalf("canceled apply changed source: %q", got)
	}
}
