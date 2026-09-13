package pinner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Change is one file update prepared by a Plan. Before and After contain the
// complete source content from when the plan was made and the intended update.
type Change struct {
	Path   string
	Before []byte
	After  []byte

	path string
	info os.FileInfo
	mode os.FileMode
}

// Plan contains the findings and file updates prepared for a fix run. Planning
// does not modify workflow files; Apply performs the prepared updates.
type Plan struct {
	Result  Result
	Changes []Change

	complete bool
}

// PlanFile resolves and prepares the update for one workflow file without
// modifying it. The returned plan can be applied with Apply.
func (p *Pinner) PlanFile(ctx context.Context, filePath string) (*Plan, error) {
	plan := &Plan{}
	if err := ctx.Err(); err != nil {
		return plan, err
	}

	path, info, err := readPlanFileInfo(filePath)
	if err != nil {
		return plan, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return plan, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	plan.Result.FilesChecked = 1

	// Recheck the path after the read. This does not eliminate every race, but
	// avoids creating a plan from bytes read through a path that has already
	// changed identity or become a symlink.
	currentInfo, err := regularFileInfo(path)
	if err != nil {
		return plan, err
	}
	if !os.SameFile(info, currentInfo) {
		return plan, fmt.Errorf("file changed while preparing plan: %s", filePath)
	}
	if info.Mode().Perm() != currentInfo.Mode().Perm() {
		return plan, fmt.Errorf("file permissions changed while preparing plan: %s", filePath)
	}
	if err := ctx.Err(); err != nil {
		return plan, err
	}

	after, findings, err := p.ProcessContent(ctx, filePath, content, true)
	if err != nil {
		return plan, err
	}
	plan.Result.UnpinnedCount = len(findings)
	plan.Result.Findings = append(plan.Result.Findings, findings...)
	if !bytes.Equal(content, after) {
		plan.Changes = append(plan.Changes, Change{
			Path:   filePath,
			Before: append([]byte(nil), content...),
			After:  append([]byte(nil), after...),
			path:   path,
			info:   currentInfo,
			mode:   currentInfo.Mode(),
		})
	}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	plan.complete = true
	return plan, nil
}

// PlanDirectory resolves and prepares all .yml and .yaml files below dirPath
// without modifying any of them. Apply is available only after every candidate
// has been planned successfully.
func (p *Pinner) PlanDirectory(ctx context.Context, dirPath string) (*Plan, error) {
	plan := &Plan{}
	if err := ctx.Err(); err != nil {
		return plan, err
	}

	cleanDirPath := filepath.Clean(dirPath)
	info, err := os.Lstat(cleanDirPath)
	if err != nil {
		return plan, fmt.Errorf("workflow directory not found: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return plan, fmt.Errorf("refusing to plan symlink directory %s", cleanDirPath)
	}
	if !info.IsDir() {
		return plan, fmt.Errorf("%s is not a directory", cleanDirPath)
	}

	err = filepath.Walk(cleanDirPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}

		filePlan, err := p.PlanFile(ctx, path)
		mergePlan(plan, filePlan)
		if err != nil {
			return fmt.Errorf("planning workflow %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return plan, err
	}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	plan.complete = true
	return plan, nil
}

// Apply writes the prepared changes. It checks every source before staging any
// replacement, then writes temporary files alongside their targets before it
// starts replacing files. If a later replacement fails, earlier replacements
// remain in place and Result.FilesModified records how many were applied.
func (plan *Plan) Apply(ctx context.Context) error {
	if plan == nil {
		return fmt.Errorf("cannot apply a nil plan")
	}
	if !plan.complete {
		return fmt.Errorf("cannot apply an incomplete plan")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	changes := make([]*Change, 0, len(plan.Changes))
	for i := range plan.Changes {
		change := &plan.Changes[i]
		if bytes.Equal(change.Before, change.After) {
			continue
		}
		if change.path == "" || change.info == nil {
			return fmt.Errorf("invalid change for %s", change.Path)
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		return nil
	}

	for _, change := range changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := preflightChange(*change); err != nil {
			return fmt.Errorf("preflighting %s: %w", change.Path, err)
		}
	}

	staged := make([]string, len(changes))
	defer func() {
		for _, path := range staged {
			if path != "" {
				removeStagedFile(path)
			}
		}
	}()
	for i, change := range changes {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("staging updates after %d file(s): %w", plan.Result.FilesModified, err)
		}
		path, err := stageChangeFile(*change)
		if err != nil {
			return fmt.Errorf("staging update for %s after %d file(s): %w", change.Path, plan.Result.FilesModified, err)
		}
		staged[i] = path
	}

	for i, change := range changes {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("applying updates after %d file(s): %w", plan.Result.FilesModified, err)
		}
		if err := preflightChange(*change); err != nil {
			return fmt.Errorf("applying updates after %d file(s): preflighting %s: %w", plan.Result.FilesModified, change.Path, err)
		}
		if err := replaceStagedFile(staged[i], change.path); err != nil {
			return fmt.Errorf("applying updates after %d file(s): replacing %s: %w", plan.Result.FilesModified, change.Path, err)
		}
		staged[i] = ""
		plan.Result.FilesModified++
	}
	return nil
}

func mergePlan(dst, src *Plan) {
	if src == nil {
		return
	}
	dst.Result.FilesChecked += src.Result.FilesChecked
	dst.Result.UnpinnedCount += src.Result.UnpinnedCount
	dst.Result.Findings = append(dst.Result.Findings, src.Result.Findings...)
	dst.Changes = append(dst.Changes, src.Changes...)
}

func readPlanFileInfo(filePath string) (string, os.FileInfo, error) {
	path, err := filepath.Abs(filePath)
	if err != nil {
		return "", nil, fmt.Errorf("resolving file path %s: %w", filePath, err)
	}
	info, err := regularFileInfo(path)
	if err != nil {
		return "", nil, err
	}
	return path, info, nil
}

func regularFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect file %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to update symlink %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to update non-regular file %s", path)
	}
	return info, nil
}

func preflightChange(change Change) error {
	info, err := regularFileInfo(change.path)
	if err != nil {
		return err
	}
	if !os.SameFile(change.info, info) {
		return fmt.Errorf("file identity changed since planning")
	}
	if change.mode.Perm() != info.Mode().Perm() {
		return fmt.Errorf("file permissions changed since planning")
	}
	content, err := os.ReadFile(change.path)
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}
	// Check once more after reading in case the path was swapped while the
	// bytes were being read.
	afterRead, err := regularFileInfo(change.path)
	if err != nil {
		return err
	}
	if !os.SameFile(change.info, afterRead) {
		return fmt.Errorf("file identity changed while reading")
	}
	if change.mode.Perm() != afterRead.Mode().Perm() {
		return fmt.Errorf("file permissions changed while reading")
	}
	if !bytes.Equal(change.Before, content) {
		return fmt.Errorf("file content changed since planning")
	}
	return nil
}

var stageChangeFile = stageChange

func stageChange(change Change) (string, error) {
	temp, err := os.CreateTemp(filepath.Dir(change.path), ".action-pin-*")
	if err != nil {
		return "", err
	}
	path := temp.Name()
	cleanup := func(err error) (string, error) {
		_ = temp.Close()
		removeStagedFile(path)
		return "", err
	}
	if err := temp.Chmod(change.mode.Perm()); err != nil {
		return cleanup(err)
	}
	if _, err := temp.Write(change.After); err != nil {
		return cleanup(err)
	}
	if err := temp.Sync(); err != nil {
		return cleanup(err)
	}
	if err := temp.Close(); err != nil {
		removeStagedFile(path)
		return "", err
	}
	return path, nil
}

func removeStagedFile(path string) {
	// On Windows, a read-only mode becomes a read-only file attribute. Make
	// pending staged files writable before cleanup so a failed batch does not
	// leave temporary files behind. Successful replacements retain the original
	// mode set in stageChange.
	_ = os.Chmod(path, 0600)
	_ = os.Remove(path)
}

var replaceStagedFile = os.Rename
