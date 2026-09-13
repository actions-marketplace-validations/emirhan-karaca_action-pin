package action

import (
	"fmt"
	"regexp"
	"strings"
)

var shaRegex = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// ActionRef represents a parsed GitHub Actions reference in a `uses:` field.
type ActionRef struct {
	Raw       string // original raw value, e.g. "actions/checkout@v4"
	Owner     string // e.g. "actions"
	Repo      string // e.g. "checkout"
	SubPath   string // e.g. "" or "sub/dir" for "owner/repo/sub/dir@v1"
	Ref       string // e.g. "v4" or 40-character commit SHA
	IsLocal   bool   // true if ref points to local path (./ or ../)
	IsDocker  bool   // true if ref is a docker action (docker://)
	IsDynamic bool   // true if ref contains dynamic expressions (${{ ... }})
	IsPinned  bool   // true if ref is already a 40-character commit SHA
}

// IsCommitSHA returns true if the given string is a 40-character hexadecimal SHA.
func IsCommitSHA(s string) bool {
	return shaRegex.MatchString(s)
}

// Parse parses a GitHub Action `uses:` string into an ActionRef.
func Parse(raw string) (*ActionRef, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty action reference")
	}

	// Check for dynamic expressions (${{ ... }})
	if strings.Contains(trimmed, "${{") {
		return &ActionRef{
			Raw:       trimmed,
			IsDynamic: true,
		}, nil
	}

	// Check for local actions
	if strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, `.\`) || strings.HasPrefix(trimmed, `..\`) || trimmed == "." {
		return &ActionRef{
			Raw:     trimmed,
			IsLocal: true,
		}, nil
	}

	// Check for docker actions
	if strings.HasPrefix(trimmed, "docker://") {
		return &ActionRef{
			Raw:      trimmed,
			IsDocker: true,
		}, nil
	}

	// Remote GitHub action: {owner}/{repo}[/{subpath}]@{ref}
	atIdx := strings.LastIndex(trimmed, "@")
	if atIdx == -1 {
		return nil, fmt.Errorf("invalid action reference (missing '@'): %s", trimmed)
	}

	target := trimmed[:atIdx]
	ref := trimmed[atIdx+1:]

	if ref == "" {
		return nil, fmt.Errorf("missing version or tag ref in action: %s", trimmed)
	}

	parts := strings.Split(target, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid action repository format (expected owner/repo): %s", trimmed)
	}

	owner := parts[0]
	repo := parts[1]
	subPath := ""
	if len(parts) > 2 {
		subPath = strings.Join(parts[2:], "/")
	}

	isPinned := IsCommitSHA(ref)

	return &ActionRef{
		Raw:      trimmed,
		Owner:    owner,
		Repo:     repo,
		SubPath:  subPath,
		Ref:      ref,
		IsLocal:  false,
		IsDocker: false,
		IsPinned: isPinned,
	}, nil
}

// PinnedString returns the uses: string formatted with the given commit SHA.
// e.g. "actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11"
func (a *ActionRef) PinnedString(sha string) string {
	if a.SubPath != "" {
		return fmt.Sprintf("%s/%s/%s@%s", a.Owner, a.Repo, a.SubPath, sha)
	}
	return fmt.Sprintf("%s/%s@%s", a.Owner, a.Repo, sha)
}

// Comment returns the comment to append to the pinned action line.
// e.g. "# v4 [pinned by action-pin]"
func (a *ActionRef) Comment() string {
	return fmt.Sprintf("# %s [pinned by action-pin]", a.Ref)
}
