package orbits

import "testing"

func TestIgnorePatterns(t *testing.T) {
	tests := []struct {
		patterns []string
		path     string
		dir      bool
		want     bool
	}{
		{[]string{"node_modules"}, "node_modules", true, true},
		{[]string{"node_modules"}, "packages/a/node_modules", true, true},
		{[]string{"*.log"}, "debug.log", false, true},
		{[]string{"*.log"}, "logs/debug.log", false, true},
		{[]string{"*.log"}, "debug.log.txt", false, false},
		{[]string{"/build"}, "build", true, true},
		{[]string{"/build"}, "src/build", true, false},
		{[]string{"docs/*.md"}, "docs/a.md", false, true},
		{[]string{"docs/*.md"}, "docs/sub/a.md", false, false},
		{[]string{"docs/**/*.md"}, "docs/sub/deep/a.md", false, true},
		{[]string{"docs/**/*.md"}, "docs/a.md", false, true},
		{[]string{"**/fixtures"}, "a/b/fixtures", true, true},
		{[]string{"secrets/**"}, "secrets/a/b", false, true},
		{[]string{"tmp/"}, "tmp", true, true},
		{[]string{"tmp/"}, "tmp", false, false},
		{[]string{"*.env", "!example.env"}, "example.env", false, false},
		{[]string{"*.env", "!example.env"}, "prod.env", false, true},
		{[]string{"file?.txt"}, "file1.txt", false, true},
		{[]string{"file[0-9].txt"}, "file7.txt", false, true},
		{[]string{"file[!0-9].txt"}, "file7.txt", false, false},
		{[]string{"# comment", "", "  "}, "anything", false, false},
		{[]string{`\#hash`}, "#hash", false, true},
		{[]string{"trailing   "}, "trailing", false, true},
		{[]string{"a.b"}, "aXb", false, false},
	}
	for _, tt := range tests {
		m := newIgnoreMatcher(tt.patterns)
		if got := m.ignored(tt.path, tt.dir); got != tt.want {
			t.Errorf("%q on %q (dir %v) = %v, want %v", tt.patterns, tt.path, tt.dir, got, tt.want)
		}
	}
}
