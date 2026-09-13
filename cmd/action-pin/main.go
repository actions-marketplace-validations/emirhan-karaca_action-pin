package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/emirhan-karaca/action-pin/internal/pinner"
	"github.com/emirhan-karaca/action-pin/internal/resolver"
)

var (
	// Version is injected during build via -ldflags "-X main.Version=..."
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func main() {
	exitCode := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(exitCode)
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithResolver(args, stdout, stderr, nil)
}

func runWithResolver(args []string, stdout, stderr io.Writer, res resolver.Resolver) int {
	fs := flag.NewFlagSet("action-pin", flag.ContinueOnError)
	fs.SetOutput(stderr)

	checkFlag := fs.Bool("check", false, "Check for unpinned actions offline (default; exits with code 1 if any exist)")
	fixFlag := fs.Bool("fix", false, "Fix workflows in place by pinning actions to commit SHAs")
	resolveFlag := fs.Bool("resolve", false, "Resolve suggested commit SHAs during checks (requires network; implied by --fix)")
	dirFlag := fs.String("dir", ".github/workflows", "Directory containing workflow files")
	fileFlag := fs.String("file", "", "Target a specific workflow file instead of a directory")
	tokenFlag := fs.String("token", "", "GitHub personal access token (defaults to GITHUB_TOKEN or GH_TOKEN env)")
	versionFlag := fs.Bool("version", false, "Print version information")
	verboseFlag := fs.Bool("verbose", false, "Enable verbose logging")

	fs.Usage = func() {
		fmt.Fprintf(stderr, "action-pin - Pin GitHub Actions to immutable commit SHAs with comment preservation\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  action-pin [flags]\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if *versionFlag {
		fmt.Fprintf(stdout, "action-pin version %s (commit: %s, built: %s)\n", Version, Commit, Date)
		return 0
	}

	if *checkFlag && *fixFlag {
		fmt.Fprintf(stderr, "Error: --check and --fix cannot be used simultaneously\n")
		return 1
	}

	// Default to check mode if neither --check nor --fix is passed
	fix := *fixFlag

	// Offline checks need neither GitHub credentials nor a resolver.
	if res == nil && (fix || *resolveFlag) {
		token := *tokenFlag
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
			if token == "" {
				token = os.Getenv("GH_TOKEN")
			}
		}
		res = resolver.New(resolver.WithToken(token))
	}

	p := pinner.New(res, pinner.WithResolve(*resolveFlag))
	ctx := context.Background()

	if *fileFlag != "" {
		filePath := *fileFlag
		findings, modified, err := p.ProcessFile(ctx, filePath, fix)
		if err != nil {
			fmt.Fprintf(stderr, "Error processing %s: %v\n", filepath.ToSlash(filePath), err)
			return 1
		}

		for _, f := range findings {
			printFinding(stdout, f, fix)
		}

		if len(findings) == 0 {
			fmt.Fprintf(stdout, "All actions in %s are already pinned to full commit SHAs.\n", filepath.ToSlash(filePath))
			return 0
		}

		if fix {
			if modified {
				fmt.Fprintf(stdout, "Successfully pinned %d action(s) in %s\n", len(findings), filepath.ToSlash(filePath))
			}
			return 0
		}

		// Check mode with findings
		fmt.Fprintf(stderr, "\nFound %d unpinned action(s) in %s. Run with --fix to update.\n", len(findings), filepath.ToSlash(filePath))
		return 1
	}

	dirExplicitlySet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "dir" {
			dirExplicitlySet = true
		}
	})

	dir := *dirFlag
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) && !dirExplicitlySet {
			fmt.Fprintf(stdout, "Directory %s does not exist. Nothing to pin.\n", filepath.ToSlash(dir))
			return 0
		}
		fmt.Fprintf(stderr, "Error: workflow directory %s: %v\n", filepath.ToSlash(dir), err)
		return 1
	}
	if !info.IsDir() {
		fmt.Fprintf(stderr, "Error: %s is not a directory. Use --file to target a single file.\n", filepath.ToSlash(dir))
		return 1
	}

	result, err := p.ProcessDirectory(ctx, dir, fix)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	if *verboseFlag || len(result.Findings) > 0 {
		for _, f := range result.Findings {
			printFinding(stdout, f, fix)
		}
	}

	if result.FilesChecked == 0 {
		fmt.Fprintf(stdout, "No workflow files found in %s\n", filepath.ToSlash(dir))
		return 0
	}

	if fix {
		if result.UnpinnedCount == 0 {
			fmt.Fprintf(stdout, "All actions are already pinned to full commit SHAs. (Checked %d file(s))\n", result.FilesChecked)
		} else {
			fmt.Fprintf(stdout, "\nSuccess: Pinned %d action(s) across %d file(s) (Checked %d file(s))\n",
				result.UnpinnedCount, result.FilesModified, result.FilesChecked)
		}
		return 0
	}

	// Check mode
	if result.UnpinnedCount == 0 {
		fmt.Fprintf(stdout, "All actions are pinned to full commit SHAs. (Checked %d file(s))\n", result.FilesChecked)
		return 0
	}

	fmt.Fprintf(stderr, "\nCheck failed: Found %d unpinned action(s) across %d file(s).\n", result.UnpinnedCount, result.FilesChecked)
	fmt.Fprintf(stderr, "Run 'action-pin --fix --dir %s' to pin them automatically.\n", filepath.ToSlash(dir))
	return 1
}

func printFinding(stdout io.Writer, f pinner.Finding, fix bool) {
	if fix {
		fmt.Fprintf(stdout, "[PINNED]   %s:%d: %s -> %s\n", filepath.ToSlash(f.File), f.Line, f.Action, f.ResolvedSHA)
		return
	}
	fmt.Fprintf(stdout, "[UNPINNED] %s:%d: %s", filepath.ToSlash(f.File), f.Line, f.Action)
	if f.ResolvedSHA != "" {
		fmt.Fprintf(stdout, " (suggested: %s)", f.ResolvedSHA)
	}
	fmt.Fprintln(stdout)
}
