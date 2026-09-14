package action_test

import (
	"testing"

	"github.com/emirhan-karaca/action-pin/v2/internal/action"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantOwner     string
		wantRepo      string
		wantSubPath   string
		wantRef       string
		wantLocal     bool
		wantDocker    bool
		wantDynamic   bool
		wantPinned    bool
		wantErr       bool
		wantPinnedStr string
		wantComment   string
	}{
		{
			name:          "standard tag reference",
			input:         "actions/checkout@v4",
			wantOwner:     "actions",
			wantRepo:      "checkout",
			wantSubPath:   "",
			wantRef:       "v4",
			wantLocal:     false,
			wantDocker:    false,
			wantPinned:    false,
			wantErr:       false,
			wantPinnedStr: "actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11",
			wantComment:   "# v4 [pinned by action-pin]",
		},
		{
			name:          "subpath in repo",
			input:         "actions/cache/restore@v3.2.1",
			wantOwner:     "actions",
			wantRepo:      "cache",
			wantSubPath:   "restore",
			wantRef:       "v3.2.1",
			wantLocal:     false,
			wantDocker:    false,
			wantPinned:    false,
			wantErr:       false,
			wantPinnedStr: "actions/cache/restore@b4ffde65f46336ab88eb53be808477a3936bae11",
			wantComment:   "# v3.2.1 [pinned by action-pin]",
		},
		{
			name:          "nested subpath workflow",
			input:         "owner/repo/.github/workflows/reusable.yml@main",
			wantOwner:     "owner",
			wantRepo:      "repo",
			wantSubPath:   ".github/workflows/reusable.yml",
			wantRef:       "main",
			wantLocal:     false,
			wantDocker:    false,
			wantPinned:    false,
			wantErr:       false,
			wantPinnedStr: "owner/repo/.github/workflows/reusable.yml@b4ffde65f46336ab88eb53be808477a3936bae11",
			wantComment:   "# main [pinned by action-pin]",
		},
		{
			name:        "already pinned 40-char commit SHA",
			input:       "actions/checkout@b4ffde65f46336ab88eb53be808477a3936bae11",
			wantOwner:   "actions",
			wantRepo:    "checkout",
			wantSubPath: "",
			wantRef:     "b4ffde65f46336ab88eb53be808477a3936bae11",
			wantLocal:   false,
			wantDocker:  false,
			wantPinned:  true,
			wantErr:     false,
		},
		{
			name:       "local action with relative path",
			input:      "./.github/actions/custom",
			wantLocal:  true,
			wantDocker: false,
			wantPinned: false,
			wantErr:    false,
		},
		{
			name:       "local action with parent path",
			input:      "../shared-action",
			wantLocal:  true,
			wantDocker: false,
			wantPinned: false,
			wantErr:    false,
		},
		{
			name:       "docker action",
			input:      "docker://alpine:3.18",
			wantLocal:  false,
			wantDocker: true,
			wantPinned: false,
			wantErr:    false,
		},
		{
			name:        "dynamic expression action",
			input:       "${{ matrix.action }}",
			wantDynamic: true,
			wantErr:     false,
		},
		{
			name:        "dynamic ref expression",
			input:       "actions/checkout@${{ matrix.version }}",
			wantDynamic: true,
			wantErr:     false,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "missing at symbol",
			input:   "actions/checkout",
			wantErr: true,
		},
		{
			name:    "missing ref after at",
			input:   "actions/checkout@",
			wantErr: true,
		},
		{
			name:    "invalid repo format without slash",
			input:   "checkout@v4",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := action.Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.IsLocal != tt.wantLocal {
				t.Errorf("IsLocal = %v, want %v", got.IsLocal, tt.wantLocal)
			}
			if got.IsDocker != tt.wantDocker {
				t.Errorf("IsDocker = %v, want %v", got.IsDocker, tt.wantDocker)
			}
			if got.IsDynamic != tt.wantDynamic {
				t.Errorf("IsDynamic = %v, want %v", got.IsDynamic, tt.wantDynamic)
			}
			if tt.wantLocal || tt.wantDocker || tt.wantDynamic {
				return
			}
			if got.Owner != tt.wantOwner {
				t.Errorf("Owner = %v, want %v", got.Owner, tt.wantOwner)
			}
			if got.Repo != tt.wantRepo {
				t.Errorf("Repo = %v, want %v", got.Repo, tt.wantRepo)
			}
			if got.SubPath != tt.wantSubPath {
				t.Errorf("SubPath = %v, want %v", got.SubPath, tt.wantSubPath)
			}
			if got.Ref != tt.wantRef {
				t.Errorf("Ref = %v, want %v", got.Ref, tt.wantRef)
			}
			if got.IsPinned != tt.wantPinned {
				t.Errorf("IsPinned = %v, want %v", got.IsPinned, tt.wantPinned)
			}
			if tt.wantPinnedStr != "" {
				pinned := got.PinnedString("b4ffde65f46336ab88eb53be808477a3936bae11")
				if pinned != tt.wantPinnedStr {
					t.Errorf("PinnedString() = %v, want %v", pinned, tt.wantPinnedStr)
				}
			}
			if tt.wantComment != "" {
				comment := got.Comment()
				if comment != tt.wantComment {
					t.Errorf("Comment() = %v, want %v", comment, tt.wantComment)
				}
			}
		})
	}
}

func TestIsCommitSHA(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"b4ffde65f46336ab88eb53be808477a3936bae11", true},
		{"B4FFDE65F46336AB88EB53BE808477A3936BAE11", true},
		{"b4ffde65f46336ab88eb53be808477a3936bae1", false},   // 39 chars
		{"b4ffde65f46336ab88eb53be808477a3936bae111", false}, // 41 chars
		{"v4", false},
		{"v4.0.0", false},
		{"main", false},
		{"b4ffde65f46336ab88eb53be808477a3936baezz", false}, // non-hex
	}

	for _, tt := range tests {
		if got := action.IsCommitSHA(tt.input); got != tt.want {
			t.Errorf("IsCommitSHA(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
