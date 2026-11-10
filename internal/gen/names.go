package gen

import (
	"strings"
	"unicode"
)

// Pascal turns a JSON name, an enum value or an operationId into a Go name: install_command is InstallCommand,
// listDnsRecords is ListDNSRecords, error-rate is ErrorRate.
func Pascal(s string) string {
	var b strings.Builder
	for _, w := range words(s) {
		if up, ok := Initialisms[strings.ToLower(w)]; ok {
			b.WriteString(up)
			continue
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}

// words splits on separators and on lower-to-upper case changes.
func words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1])):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

// Singular turns a plural Go name into the singular, for an array's items: Countries is Country, Statuses is Status.
func Singular(s string) string {
	switch {
	case strings.HasSuffix(s, "People"):
		return strings.TrimSuffix(s, "People") + "Person"
	case strings.HasSuffix(s, "ies"):
		return strings.TrimSuffix(s, "ies") + "y"
	case strings.HasSuffix(s, "uses"), strings.HasSuffix(s, "sses"), strings.HasSuffix(s, "ches"),
		strings.HasSuffix(s, "shes"), strings.HasSuffix(s, "xes"):
		return strings.TrimSuffix(s, "es")
	case strings.HasSuffix(s, "IDs"), strings.HasSuffix(s, "URLs"):
		return strings.TrimSuffix(s, "s")
	case strings.HasSuffix(s, "ss"):
		return s
	case strings.HasSuffix(s, "s") && len(s) > 3:
		return strings.TrimSuffix(s, "s")
	}
	return s
}
