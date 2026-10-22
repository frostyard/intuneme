package broker

import (
	"errors"
	"fmt"
	"strings"
)

// The helpers in this file are a test-only port of the two parsing layers
// dbus-daemon applies to an Exec value in a service file:
//
//  1. Service-file string unescaping: bus/desktop-file.c, unescape_string and
//     the value handling in parse_key_value.
//  2. Command line splitting and unquoting: dbus/dbus-shell.c,
//     tokenize_command_line and _dbus_shell_unquote.
//
// They exist so tests prove that generated service files load and produce the
// intended argv, instead of only comparing output strings.

// serviceFileExec returns the argv dbus-daemon would run for the Exec line of
// the given service file content.
func serviceFileExec(content string) ([]string, error) {
	var value string
	found := false
	for _, line := range strings.Split(content, "\n") {
		// parse_key_value skips spaces after '='.
		if strings.HasPrefix(line, "Exec=") {
			value = strings.TrimLeft(line[len("Exec="):], " ")
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("service file has no Exec line")
	}
	unescaped, err := serviceFileUnescape(value)
	if err != nil {
		return nil, err
	}
	return shellParseArgv(unescaped)
}

// serviceFileUnescape is a port of unescape_string in bus/desktop-file.c.
// Only \s, \t, \n, \r and \\ are valid. An embedded NUL, an escape at the end,
// or any other escape makes the whole service file invalid.
func serviceFileUnescape(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case 0:
			return "", errors.New("text contains embedded NUL")
		case '\\':
			i++
			if i >= len(s) {
				return "", errors.New(`text ended in \`)
			}
			switch s[i] {
			case 's':
				b.WriteByte(' ')
			case 't':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			default:
				return "", fmt.Errorf("invalid escape sequence \\%c", s[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// shellParseArgv is a port of _dbus_shell_parse_argv in dbus/dbus-shell.c for
// the input the tokenizer and unquoter accept. It does not do the shell
// expansions dbus does not do either.
func shellParseArgv(commandLine string) ([]string, error) {
	var tokens []string
	var token strings.Builder
	var quote byte
	quoted := false // parity of consecutive backslashes, for \" handling

	for i := 0; i < len(commandLine); i++ {
		c := commandLine[i]
		switch {
		case quote == '\\':
			if c != '\n' {
				token.WriteByte('\\')
				token.WriteByte(c)
			}
			quote = 0
		case quote == '#':
			// Discard up to and including the next newline.
			if c == '\n' {
				quote = 0
			}
		case quote != 0:
			if c == quote && (quote != '"' || !quoted) {
				quote = 0
			}
			token.WriteByte(c)
		default:
			switch c {
			case '\n':
				tokens = append(tokens, token.String())
				token.Reset()
			case ' ', '\t':
				if token.Len() > 0 {
					tokens = append(tokens, token.String())
					token.Reset()
				}
			case '\'', '"':
				token.WriteByte(c)
				quote = c
			case '#':
				quote = '#'
			case '\\':
				quote = '\\'
			default:
				token.WriteByte(c)
			}
		}
		if c == '\\' {
			quoted = !quoted
		} else {
			quoted = false
		}
	}

	tokens = append(tokens, token.String())
	if quote != 0 {
		return nil, errors.New("unclosed quotes in command line")
	}

	argv := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		arg, ok := shellUnquote(tok)
		if !ok {
			return nil, fmt.Errorf("cannot unquote token %q", tok)
		}
		argv = append(argv, arg)
	}
	return argv, nil
}

// shellUnquote is a port of _dbus_shell_unquote in dbus/dbus-shell.c.
func shellUnquote(s string) (string, bool) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		// Append non-quoted characters, honoring backslash escapes. An
		// escaped newline outside quotes disappears.
		for i < len(s) && s[i] != '"' && s[i] != '\'' {
			if s[i] == '\\' {
				i++
				if i < len(s) {
					if s[i] != '\n' {
						b.WriteByte(s[i])
					}
					i++
				}
				continue
			}
			b.WriteByte(s[i])
			i++
		}
		if i < len(s) {
			value, next, ok := shellUnquoteInPlace(s[i:])
			if !ok {
				return "", false
			}
			b.WriteString(value)
			i += next
		}
	}
	return b.String(), true
}

// shellUnquoteInPlace is a port of unquote_string_inplace in
// dbus/dbus-shell.c. It returns the unquoted value and the number of input
// bytes consumed, including the closing quote.
func shellUnquoteInPlace(s string) (value string, next int, ok bool) {
	if s == "" || (s[0] != '"' && s[0] != '\'') {
		return "", 0, false
	}
	quote := s[0]
	var b strings.Builder
	i := 1
	if quote == '"' {
		for i < len(s) {
			switch s[i] {
			case '"':
				return b.String(), i + 1, true
			case '\\':
				i++
				if i >= len(s) {
					b.WriteByte('\\')
					return b.String(), i, false
				}
				switch s[i] {
				case '"', '\\', '`', '$', '\n':
					b.WriteByte(s[i])
					i++
				default:
					// Not an escaped character: keep the backslash and
					// let the next iteration handle this byte.
					b.WriteByte('\\')
				}
			default:
				b.WriteByte(s[i])
				i++
			}
		}
	} else {
		for i < len(s) {
			if s[i] == '\'' {
				return b.String(), i + 1, true
			}
			b.WriteByte(s[i])
			i++
		}
	}
	// The closing quote was never found.
	return b.String(), i, false
}
