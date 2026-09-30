package catalog

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseProperties reads a properties file as java.util.Properties.load(Reader) does: logical lines
// joined by a trailing backslash, comments starting with # or !, a key ended by an unescaped =, :
// or white space, and the escapes \t \n \f \r \uXXXX. A key given twice is an error, where
// Properties would keep the second.
func ParseProperties(text string) (map[string]string, error) {
	props := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		lineNo := i + 1
		line := strings.TrimLeft(lines[i], " \t\f")
		if line == "" || line[0] == '#' || line[0] == '!' {
			continue
		}
		for endsInContinuation(line) && i+1 < len(lines) {
			i++
			line = line[:len(line)-1] + strings.TrimLeft(lines[i], " \t\f")
		}
		key, rest := splitKey(line)
		k, err := unescape(key)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		v, err := unescape(rest)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		if _, dup := props[k]; dup {
			return nil, fmt.Errorf("line %d: key %q appears more than once", lineNo, k)
		}
		props[k] = v
	}
	return props, nil
}

// endsInContinuation reports whether a line ends in an odd number of backslashes.
func endsInContinuation(line string) bool {
	n := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}

func splitKey(line string) (key, value string) {
	i := 0
	for i < len(line) {
		c := line[i]
		if c == '\\' {
			i += 2
			continue
		}
		if c == '=' || c == ':' || c == ' ' || c == '\t' || c == '\f' {
			break
		}
		i++
	}
	if i > len(line) {
		i = len(line)
	}
	key = line[:i]
	rest := strings.TrimLeft(line[i:], " \t\f")
	if rest != "" && (rest[0] == '=' || rest[0] == ':') {
		rest = strings.TrimLeft(rest[1:], " \t\f")
	}
	return key, rest
}

func unescape(s string) (string, error) {
	if !strings.Contains(s, `\`) {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 == len(s) {
			b.WriteByte(c)
			continue
		}
		i++
		switch s[i] {
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case 'u':
			if i+5 > len(s) {
				return "", fmt.Errorf("short \\u escape")
			}
			r, err := strconv.ParseUint(s[i+1:i+5], 16, 32)
			if err != nil {
				return "", fmt.Errorf("invalid \\u escape %q", s[i+1:i+5])
			}
			b.WriteRune(rune(r))
			i += 4
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}
