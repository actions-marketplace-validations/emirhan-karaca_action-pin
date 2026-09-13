package pinner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/emirhan-karaca/action-pin/internal/action"
	"github.com/emirhan-karaca/action-pin/internal/resolver"
)

// Finding describes an unpinned action found in a workflow.
type Finding struct {
	File         string `json:"file"`
	Line         int    `json:"line"`
	Column       int    `json:"column"`
	Action       string `json:"action"`        // original uses: string (e.g. actions/checkout@v4)
	Owner        string `json:"owner"`         // repo owner
	Repo         string `json:"repo"`          // repo name
	Ref          string `json:"ref"`           // tag or branch name
	ResolvedSHA  string `json:"resolved_sha"`  // 40-char commit SHA
	PinnedAction string `json:"pinned_action"` // new uses: string (e.g. actions/checkout@sha)
}

// Result summarizes a check or fix run across files.
type Result struct {
	FilesChecked  int       `json:"files_checked"`
	FilesModified int       `json:"files_modified"`
	UnpinnedCount int       `json:"unpinned_count"`
	Findings      []Finding `json:"findings"`
}

// Pinner coordinates reading, AST traversing, resolving, and updating workflow files.
type Pinner struct {
	resolver resolver.Resolver
}

// New creates a new Pinner with the provided resolver.
func New(r resolver.Resolver) *Pinner {
	return &Pinner{resolver: r}
}

// ProcessContent processes YAML content, identifying and optionally pinning actions.
// If fix is true and unpinned actions are found, it returns the updated YAML content.
// If fix is false or no changes are made, it returns the original content.
func (p *Pinner) ProcessContent(ctx context.Context, filename string, content []byte, fix bool) ([]byte, []Finding, error) {
	if len(bytes.TrimSpace(content)) == 0 {
		return content, nil, nil
	}

	dec := yaml.NewDecoder(bytes.NewReader(content))
	var docs []*yaml.Node
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("failed to parse YAML in %s: %w", filename, err)
		}
		docs = append(docs, &doc)
	}

	var findings []Finding
	for _, doc := range docs {
		docFindings, err := p.traverseAndPin(ctx, filename, doc, fix)
		if err != nil {
			return nil, nil, err
		}
		findings = append(findings, docFindings...)
	}

	if !fix || len(findings) == 0 {
		return content, findings, nil
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return nil, nil, fmt.Errorf("failed to encode YAML for %s: %w", filename, err)
		}
	}
	if err := enc.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	return buf.Bytes(), findings, nil
}

// traverseAndPin searches a YAML node tree for `uses:` keys and pins them.
func (p *Pinner) traverseAndPin(ctx context.Context, filename string, node *yaml.Node, fix bool) ([]Finding, error) {
	var findings []Finding

	var walk func(n *yaml.Node) error
	walk = func(n *yaml.Node) error {
		if n == nil {
			return nil
		}

		if n.Kind == yaml.MappingNode {
			// In MappingNode, Content holds key/value pairs: [k0, v0, k1, v1, ...]
			for i := 0; i < len(n.Content); i += 2 {
				keyNode := n.Content[i]
				valNode := n.Content[i+1]

				if keyNode.Kind == yaml.ScalarNode && keyNode.Value == "uses" && valNode.Kind == yaml.ScalarNode {
					actRef, err := action.Parse(valNode.Value)
					if err == nil && !actRef.IsLocal && !actRef.IsDocker && !actRef.IsDynamic && !actRef.IsPinned {
						// Found an unpinned remote action
						sha, err := p.resolver.Resolve(ctx, actRef.Owner, actRef.Repo, actRef.Ref)
						if err != nil {
							return fmt.Errorf("resolving %s on line %d in %s: %w", valNode.Value, valNode.Line, filename, err)
						}

						finding := Finding{
							File:         filepath.ToSlash(filename),
							Line:         valNode.Line,
							Column:       valNode.Column,
							Action:       valNode.Value,
							Owner:        actRef.Owner,
							Repo:         actRef.Repo,
							Ref:          actRef.Ref,
							ResolvedSHA:  sha,
							PinnedAction: actRef.PinnedString(sha),
						}
						findings = append(findings, finding)

						if fix {
							valNode.Value = actRef.PinnedString(sha)
							valNode.LineComment = actRef.Comment()
						}
					}
				}

				// Recurse into value node
				if err := walk(valNode); err != nil {
					return err
				}
			}
			return nil
		}

		for _, child := range n.Content {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(node); err != nil {
		return nil, err
	}
	return findings, nil
}

// ProcessFile processes a single workflow file.
func (p *Pinner) ProcessFile(ctx context.Context, filePath string, fix bool) ([]Finding, bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	newContent, findings, err := p.ProcessContent(ctx, filePath, content, fix)
	if err != nil {
		return nil, false, err
	}

	modified := false
	if fix && len(findings) > 0 && !bytes.Equal(content, newContent) {
		if err := os.WriteFile(filePath, newContent, 0644); err != nil {
			return nil, false, fmt.Errorf("failed to write updated file %s: %w", filePath, err)
		}
		modified = true
	}

	return findings, modified, nil
}

// ProcessDirectory scans a directory (and subdirectories) for .yml and .yaml workflow files.
func (p *Pinner) ProcessDirectory(ctx context.Context, dirPath string, fix bool) (*Result, error) {
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, fmt.Errorf("workflow directory not found: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dirPath)
	}

	result := &Result{}

	err = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yml" && ext != ".yaml" {
			return nil
		}

		result.FilesChecked++
		findings, modified, err := p.ProcessFile(ctx, path, fix)
		if err != nil {
			return err
		}

		if modified {
			result.FilesModified++
		}
		result.UnpinnedCount += len(findings)
		result.Findings = append(result.Findings, findings...)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}
