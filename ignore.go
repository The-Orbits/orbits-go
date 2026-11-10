package orbits

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// IgnoreFile is the file in a project's folder whose patterns UploadDir and ZipDir leave out of the upload. Its
// syntax is .gitignore's: one pattern per line, # for comments, ! to include again what an earlier pattern left
// out, a trailing / for folders only, a / elsewhere to anchor the pattern to the folder's root, and *, ?, [abc] and
// ** wildcards.
const IgnoreFile = ".orbitsignore"

// DefaultIgnore is what an upload always leaves out, wherever it appears: dependencies and build output the
// builder makes itself, version control, macOS metadata, and the CLI's link to the project. Orbits drops the same
// folders from a zip, so sending them only costs time.
var DefaultIgnore = []string{"node_modules", ".git", ".next", "__MACOSX", ".DS_Store", ".orbits"}

// ignorePattern is one compiled line.
type ignorePattern struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// ignoreMatcher decides which paths are left out. Later patterns win, as in .gitignore.
type ignoreMatcher struct {
	patterns []ignorePattern
}

func newIgnoreMatcher(lines []string) *ignoreMatcher {
	m := &ignoreMatcher{}
	for _, l := range lines {
		if p, ok := compileIgnore(l); ok {
			m.patterns = append(m.patterns, p)
		}
	}
	return m
}

// readIgnoreLines reads a .orbitsignore.
func readIgnoreLines(r io.Reader) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

// ignored reports whether rel (slash-separated, relative to the root) is left out.
func (m *ignoreMatcher) ignored(rel string, isDir bool) bool {
	out := false
	for _, p := range m.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if p.re.MatchString(rel) {
			out = !p.negate
		}
	}
	return out
}

func compileIgnore(line string) (ignorePattern, bool) {
	line = strings.TrimRight(line, "\r")
	// Trailing spaces go unless escaped.
	for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, `\ `) {
		line = line[:len(line)-1]
	}
	if line == "" || strings.HasPrefix(line, "#") {
		return ignorePattern{}, false
	}
	var p ignorePattern
	if strings.HasPrefix(line, "!") {
		p.negate = true
		line = line[1:]
	} else if strings.HasPrefix(line, `\!`) || strings.HasPrefix(line, `\#`) {
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimRight(line, "/")
	}
	if line == "" {
		return ignorePattern{}, false
	}
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '*' && strings.HasPrefix(line[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case c == '*' && strings.HasPrefix(line[i:], "**") && i+2 == len(line):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			end := strings.IndexByte(line[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := line[i+1 : i+1+end]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += end + 1
		case c == '\\' && i+1 < len(line):
			i++
			b.WriteString(regexp.QuoteMeta(string(line[i])))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return ignorePattern{}, false
	}
	p.re = re
	return p, true
}
