// Package diff formats in-memory file changes as unified patches.
package diff

import (
	"bytes"
	"fmt"
)

const contextLines = 3

// Unified returns a standard unified patch for path. Path is a logical Git path
// (normally slash-separated and relative to the caller's working directory).
// It returns nil when before and after are identical.
//
// The formatter deliberately emits one hunk per changed file. That keeps the
// implementation small while retaining enough surrounding context for patches
// to be readable and applicable.
func Unified(path string, before, after []byte) []byte {
	if bytes.Equal(before, after) {
		return nil
	}

	oldLines := splitLines(before)
	newLines := splitLines(after)
	prefix, suffix := commonEdges(oldLines, newLines)

	oldChangedEnd := len(oldLines) - suffix
	newChangedEnd := len(newLines) - suffix
	oldStart := max(0, prefix-contextLines)
	newStart := max(0, prefix-contextLines)
	oldEnd := min(len(oldLines), oldChangedEnd+contextLines)
	newEnd := min(len(newLines), newChangedEnd+contextLines)

	var out bytes.Buffer
	fmt.Fprintf(&out, "--- %s\n", QuotePath("a/"+path))
	fmt.Fprintf(&out, "+++ %s\n", QuotePath("b/"+path))
	fmt.Fprintf(&out, "@@ -%s +%s @@\n", hunkRange(oldStart, oldEnd-oldStart), hunkRange(newStart, newEnd-newStart))

	for _, line := range oldLines[oldStart:prefix] {
		writeLine(&out, ' ', line)
	}
	for _, line := range oldLines[prefix:oldChangedEnd] {
		writeLine(&out, '-', line)
	}
	for _, line := range newLines[prefix:newChangedEnd] {
		writeLine(&out, '+', line)
	}
	for _, line := range oldLines[oldChangedEnd:oldEnd] {
		writeLine(&out, ' ', line)
	}

	return out.Bytes()
}

// QuotePath quotes a path using Git's C-style quoting when the path contains
// whitespace, control characters, quotes, backslashes, or non-ASCII bytes.
// It is suitable for the --- and +++ paths in a unified patch.
func QuotePath(path string) string {
	needsQuote := false
	for _, b := range []byte(path) {
		if b <= ' ' || b == 0x7f || b >= 0x80 || b == '"' || b == '\\' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return path
	}

	var out bytes.Buffer
	out.WriteByte('"')
	for _, b := range []byte(path) {
		switch b {
		case '\\':
			out.WriteString(`\\`)
		case '"':
			out.WriteString(`\"`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if b < 0x20 || b == 0x7f || b >= 0x80 {
				fmt.Fprintf(&out, `\%03o`, b)
			} else {
				out.WriteByte(b)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}

type line struct {
	data       []byte
	terminated bool
}

func splitLines(content []byte) []line {
	if len(content) == 0 {
		return nil
	}

	var lines []line
	start := 0
	for start < len(content) {
		next := bytes.IndexByte(content[start:], '\n')
		if next < 0 {
			lines = append(lines, line{data: content[start:]})
			break
		}
		end := start + next + 1
		lines = append(lines, line{data: content[start:end], terminated: true})
		start = end
	}
	return lines
}

func commonEdges(oldLines, newLines []line) (prefix, suffix int) {
	for prefix < len(oldLines) && prefix < len(newLines) && equalLine(oldLines[prefix], newLines[prefix]) {
		prefix++
	}
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		equalLine(oldLines[len(oldLines)-1-suffix], newLines[len(newLines)-1-suffix]) {
		suffix++
	}
	return prefix, suffix
}

func equalLine(left, right line) bool {
	return left.terminated == right.terminated && bytes.Equal(left.data, right.data)
}

func hunkRange(start, count int) string {
	if count == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	return fmt.Sprintf("%d,%d", start+1, count)
}

func writeLine(out *bytes.Buffer, prefix byte, line line) {
	out.WriteByte(prefix)
	out.Write(line.data)
	if !line.terminated {
		out.WriteString("\n\\ No newline at end of file\n")
	}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
