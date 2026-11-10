package orbits

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestVersionMatchesChangelog keeps Version (the User-Agent) in step with the newest released heading.
func TestVersionMatchesChangelog(t *testing.T) {
	data, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## v(\d+\.\d+\.\d+) `).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("CHANGELOG.md has no ## vX.Y.Z heading")
	}
	if m[1] != Version {
		t.Fatalf("CHANGELOG.md's newest version is %s, Version is %s", m[1], Version)
	}
}

// repoFiles lists the repository's files, without .git.
func repoFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestNoSyncCopies fails on the " 2" copies file sync makes on macOS, which break the build.
func TestNoSyncCopies(t *testing.T) {
	copyName := regexp.MustCompile(` [0-9]+(\.[A-Za-z0-9]+)?$`)
	for _, f := range repoFiles(t) {
		if copyName.MatchString(filepath.Base(f)) {
			t.Errorf("%s looks like a sync copy: delete it", f)
		}
	}
}

// TestRepositoryWording keeps the repository free of private host names and addresses, and of wording that does
// not belong in a public SDK. The words are assembled here so this file does not match itself.
func TestRepositoryWording(t *testing.T) {
	join := func(parts ...string) string { return strings.Join(parts, "") }
	banned := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b` + join("clau", "de") + `\b`),
		regexp.MustCompile(`(?i)` + join("anth", "ropic")),
		regexp.MustCompile(`(?i)` + join("cloud", "stack")),
		regexp.MustCompile(`(?i)\b` + join("cod", "ex") + `\b`),
		regexp.MustCompile(`(?i)\b` + join("open", "ai") + `\b`),
		regexp.MustCompile(`(?i)\b` + join("chat", "gpt") + `\b`),
		regexp.MustCompile(join("AGE", "NTS") + `\.md`),
		regexp.MustCompile(`(?i)generated (by|with) (an )?ai\b`),
		// Private networks and internal host names.
		regexp.MustCompile(`\b(10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b`),
		regexp.MustCompile(`(?i)\b[a-z0-9-]+\.(internal|svc\.cluster\.local|corp|lan)\b`),
	}
	for _, f := range repoFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, re := range banned {
			if loc := re.FindIndex(data); loc != nil {
				line := 1 + strings.Count(string(data[:loc[0]]), "\n")
				t.Errorf("%s:%d: %q is not allowed in this repository", f, line, data[loc[0]:loc[1]])
			}
		}
	}
}

// TestNoDependencies keeps the module on the standard library.
func TestNoDependencies(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"require", "replace"} {
		if regexp.MustCompile(`(?m)^` + word + `\b`).Match(data) {
			t.Errorf("go.mod has a %s directive: the SDK has no dependencies", word)
		}
	}
	if _, err := os.Stat("go.work"); err == nil {
		t.Error("go.work must not be committed")
	}
}
