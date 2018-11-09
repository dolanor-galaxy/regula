package sexpr

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsWhitespace(t *testing.T) {
	require.True(t, isWhitespace(' '))
	require.True(t, isWhitespace('\v'))
	require.True(t, isWhitespace('\f'))
	require.True(t, isWhitespace('\t'))
	require.True(t, isWhitespace('\r'))
	require.True(t, isWhitespace('\n'))
	require.False(t, isWhitespace('-'))
	require.False(t, isWhitespace('a'))
	require.False(t, isWhitespace('"'))
	require.False(t, isWhitespace('('))
	require.False(t, isWhitespace(')'))
	require.False(t, isWhitespace('_'))
	require.False(t, isWhitespace('0'))
	require.False(t, isWhitespace('#'))
	require.False(t, isWhitespace(';'))
}

func TestIsLParen(t *testing.T) {
	require.True(t, isLParen('('))
	require.False(t, isLParen(' '))
	require.False(t, isLParen('\t'))
	require.False(t, isLParen('\r'))
	require.False(t, isLParen('\n'))
	require.False(t, isLParen('-'))
	require.False(t, isLParen('a'))
	require.False(t, isLParen('"'))
	require.False(t, isLParen(')'))
	require.False(t, isLParen('_'))
	require.False(t, isLParen('0'))
	require.False(t, isLParen('#'))
	require.False(t, isLParen(';'))
}

func TestIsRParen(t *testing.T) {
	require.True(t, isRParen(')'))
	require.False(t, isRParen(' '))
	require.False(t, isRParen('\t'))
	require.False(t, isRParen('\r'))
	require.False(t, isRParen('\n'))
	require.False(t, isRParen('-'))
	require.False(t, isRParen('a'))
	require.False(t, isRParen('"'))
	require.False(t, isRParen('('))
	require.False(t, isRParen('_'))
	require.False(t, isRParen('0'))
	require.False(t, isRParen('#'))
	require.False(t, isRParen(';'))
}

func TestIsString(t *testing.T) {
	require.True(t, isString('"'))
	require.False(t, isString(' '))
	require.False(t, isString('\t'))
	require.False(t, isString('\r'))
	require.False(t, isString('\n'))
	require.False(t, isString('-'))
	require.False(t, isString('a'))
	require.False(t, isString(')'))
	require.False(t, isString('('))
	require.False(t, isString('_'))
	require.False(t, isString('0'))
	require.False(t, isString('#'))
	require.False(t, isString(';'))
}

func TestIsNumber(t *testing.T) {
	require.True(t, isNumber('-'))
	for r := '0'; r <= '9'; r++ {
		require.True(t, isNumber(r))
	}
	require.False(t, isNumber('"'))
	require.False(t, isNumber(' '))
	require.False(t, isNumber('\t'))
	require.False(t, isNumber('\r'))
	require.False(t, isNumber('\n'))
	require.False(t, isNumber('a'))
	require.False(t, isNumber(')'))
	require.False(t, isNumber('('))
	require.False(t, isNumber('_'))
	require.False(t, isNumber('#'))
	require.False(t, isNumber(';'))
}

func TestIsBool(t *testing.T) {
	require.True(t, isBool('#'))
	require.False(t, isBool('"'))
	require.False(t, isBool(' '))
	require.False(t, isBool('\t'))
	require.False(t, isBool('\r'))
	require.False(t, isBool('\n'))
	require.False(t, isBool('-'))
	require.False(t, isBool('a'))
	require.False(t, isBool(')'))
	require.False(t, isBool('('))
	require.False(t, isBool('_'))
	require.False(t, isBool('0'))
	require.False(t, isBool(';'))
}

func TestIsComment(t *testing.T) {
	require.True(t, isComment(';'))
	require.False(t, isComment('#'))
	require.False(t, isComment('"'))
	require.False(t, isComment(' '))
	require.False(t, isComment('\t'))
	require.False(t, isComment('\r'))
	require.False(t, isComment('\n'))
	require.False(t, isComment('-'))
	require.False(t, isComment('a'))
	require.False(t, isComment(')'))
	require.False(t, isComment('('))
	require.False(t, isComment('_'))
	require.False(t, isComment('0'))
}

func TestIsSymbol(t *testing.T) {
	require.True(t, isSymbol('a'))
	require.True(t, isSymbol('Z'))
	require.True(t, isSymbol('!'))
	require.True(t, isSymbol('+'))
	require.True(t, isSymbol('_'))

	require.False(t, isSymbol(';'))
	require.False(t, isSymbol('#'))
	require.False(t, isSymbol('"'))
	require.False(t, isSymbol(' '))
	require.False(t, isSymbol('\t'))
	require.False(t, isSymbol('\r'))
	require.False(t, isSymbol('\n'))

	// '-' is a special case because it can also denote a number -
	// we'll have to handle this in the parser
	require.False(t, isSymbol('-'))

	require.False(t, isSymbol(')'))
	require.False(t, isSymbol('('))
	require.False(t, isSymbol('0'))
}

// NewScanner wraps an io.Reader
func TestNewScanner(t *testing.T) {
	expected := "(+ 1 1)"
	b := bytes.NewBufferString(expected)
	s := NewScanner(b)
	content, err := s.r.ReadString('\n')
	require.Error(t, err)
	require.Equal(t, io.EOF, err)
	require.Equal(t, expected, content)
}

func assertScannerScanned(t *testing.T, s *Scanner, output string, token Token, byteCount, charCount, lineCount, lineCharCount int) {
	tok, lit, err := s.Scan()
	require.NoError(t, err)
	require.Equalf(t, token, tok, "token")
	require.Equalf(t, output, lit, "literal")
	require.Equalf(t, byteCount, s.byteCount, "byteCount")
	require.Equalf(t, charCount, s.charCount, "charCount")
	require.Equalf(t, lineCount, s.lineCount, "lineCount")
	require.Equalf(t, lineCharCount, s.lineCharCount, "lineCharCount")
}

func assertScanned(t *testing.T, input, output string, token Token, byteCount, charCount, lineCount, lineCharCount int) {
	t.Run(fmt.Sprintf("Scan %s 0x%x", input, input), func(t *testing.T) {
		b := bytes.NewBufferString(input)
		s := NewScanner(b)
		assertScannerScanned(t, s, output, token, byteCount, charCount, lineCount, lineCharCount)
	})
}

func assertScannerScanFailed(t *testing.T, s *Scanner, message string) {
	_, _, err := s.Scan()
	require.EqualError(t, err, message)

}

func assertScanFailed(t *testing.T, input, message string) {
	t.Run(fmt.Sprintf("Scan should fail %s 0x%x", input, input), func(t *testing.T) {
		b := bytes.NewBufferString(input)
		s := NewScanner(b)
		assertScannerScanFailed(t, s, message)
	})

}

func TestScannerScanParenthesis(t *testing.T) {
	// Test L Parenthesis
	assertScanned(t, "(", "(", LPAREN, 1, 1, 1, 1)
	// Test R Parenthesis
	assertScanned(t, ")", ")", RPAREN, 1, 1, 1, 1)
}

func TestScannerScanWhiteSpace(t *testing.T) {
	// Test white-space
	assertScanned(t, " ", " ", WHITESPACE, 1, 1, 1, 1)
	assertScanned(t, "\t", "\t", WHITESPACE, 1, 1, 1, 1)
	assertScanned(t, "\r", "\r", WHITESPACE, 1, 1, 1, 1)
	assertScanned(t, "\n", "\n", WHITESPACE, 1, 1, 2, 0)
	assertScanned(t, "\v", "\v", WHITESPACE, 1, 1, 1, 1)
	assertScanned(t, "\f", "\f", WHITESPACE, 1, 1, 1, 1)
	// Test contiguous white-space:
	// - terminated by EOF
	assertScanned(t, "  ", "  ", WHITESPACE, 2, 2, 1, 2)
	// - terminated by non white-space character.
	assertScanned(t, "  (", "  ", WHITESPACE, 2, 2, 1, 2)
}

func TestScannerScanString(t *testing.T) {
	// Test string:
	// - the empty string
	assertScanned(t, `""`, "", STRING, 2, 2, 1, 2)
	// - the happy case
	assertScanned(t, `"foo"`, "foo", STRING, 5, 5, 1, 5)
	// - an unterminated sad case
	assertScanFailed(t, `"foo`, "Error:1,4: unterminated string constant")
	// - happy case with escaped double quote
	assertScanned(t, `"foo\""`, `foo"`, STRING, 7, 7, 1, 7)
	// - sad case with escaped terminator
	assertScanFailed(t, `"foo\"`, "Error:1,6: unterminated string constant")
}

func TestScannerScanNumber(t *testing.T) {
	// Test number
	// - Single digit integer, EOF terminated
	assertScanned(t, "1", "1", NUMBER, 1, 1, 1, 1)
	// - Single digit integer, terminated by non-numeric character
	assertScanned(t, "1)", "1", NUMBER, 1, 1, 1, 1)
	// - Multi-digit integer, EOF terminated
	assertScanned(t, "998989", "998989", NUMBER, 6, 6, 1, 6)
	// - Negative multi-digit integer, EOF terminated
	assertScanned(t, "-100", "-100", NUMBER, 4, 4, 1, 4)
	// - Floating point number, EOF terminated
	assertScanned(t, "2.4", "2.4", NUMBER, 3, 3, 1, 3)
	// - long negative float, terminated by non-numeric character
	assertScanned(t, "-123.45456 ", "-123.45456", NUMBER, 10, 10, 1, 10)
	// - special case: a "-" without a number following it (as per the minus operator)
	assertScanned(t, "- 1 2", "-", SYMBOL, 1, 1, 1, 1)
	// - sad case: a minus mid-number
	assertScanFailed(t, "1-2", "Error:1,2: invalid number format (minus can only appear at the beginning of a number)")
	// - sad case: a minus followed by EOF
	assertScanFailed(t, "-", "Error:1,1: EOF")
}

func TestScannerScanBool(t *testing.T) {
	// Happy cases
	// - true,  EOF Terminated
	assertScanned(t, "#true", "true", BOOL, 5, 5, 1, 5)
	// - false, newline terminated
	assertScanned(t, "#false\n", "false", BOOL, 7, 7, 2, 0)
	// Sad cases
	// - partial true
	assertScanFailed(t, "#tru ", "Error:1,4: invalid boolean: tru")
	// - partial false
	assertScanFailed(t, "#fa)", "Error:1,3: invalid boolean: fa")
	// - invalid
	assertScanFailed(t, "#1", "Error:1,1: invalid boolean")
	// - repeated signal character
	assertScanFailed(t, "##", "Error:1,1: invalid boolean")
	// - empty
	assertScanFailed(t, "#", "Error:1,1: invalid boolean")
}

func TestScannerScanComment(t *testing.T) {
	// Simple empty comment at EOF
	assertScanned(t, ";", "", COMMENT, 1, 1, 1, 1)
	// Comment terminated by newline
	assertScanned(t, "; Foo\nbar", " Foo", COMMENT, 6, 6, 2, 0)
	// Comment containing Comment char
	assertScanned(t, ";Pants;On;Fire", "Pants;On;Fire", COMMENT, 14, 14, 1, 14)
	// Comment containing control characters
	assertScanned(t, `;()"-#1`, `()"-#1`, COMMENT, 7, 7, 1, 7)
}

func TestScannerScanSymbol(t *testing.T) {
	// Simple, single character identifier
	assertScanned(t, "a", "a", SYMBOL, 1, 1, 1, 1)
	// Fully formed symbol
	assertScanned(t, "abba-sucks-123_ok!", "abba-sucks-123_ok!", SYMBOL, 18, 18, 1, 18)
	// Unicode in symbols
	assertScanned(t, "mötlěy_crü_sucks_more", "mötlěy_crü_sucks_more", SYMBOL, 24, 21, 1, 21)
	// terminated by comment
	assertScanned(t, "bon;jovi is worse", "bon", SYMBOL, 3, 3, 1, 3)
	// terminated by whitespace
	assertScanned(t, "van halen is the worst", "van", SYMBOL, 3, 3, 1, 3)
	// terminated by control character
	assertScanned(t, "NoWayMichaelBolton)IsTheNadir", "NoWayMichaelBolton", SYMBOL, 18, 18, 1, 18)
	// symbol starting with a non-alpha character
	assertScanned(t, "+", "+", SYMBOL, 1, 1, 1, 1)
	// actually handled by the number scan, but we'll check '-' all the same:
	assertScanned(t, "-", "-", SYMBOL, 1, 1, 1, 1)
}

func TestScannerScanSequence(t *testing.T) {
	input := `
(and
  (= (+ 1 -1) 0)
  (= my-parameter "fudge sundae")) ; Crazy
`
	b := bytes.NewBufferString(input)
	s := NewScanner(b)
	assertScannerScanned(t, s, "\n", WHITESPACE, 1, 1, 2, 0)
	assertScannerScanned(t, s, "(", LPAREN, 2, 2, 2, 1)
	assertScannerScanned(t, s, "and", SYMBOL, 5, 5, 2, 5)
	assertScannerScanned(t, s, "\n  ", WHITESPACE, 8, 8, 3, 2)
	assertScannerScanned(t, s, "(", LPAREN, 9, 9, 3, 3)
	assertScannerScanned(t, s, "=", SYMBOL, 10, 10, 3, 4)
	assertScannerScanned(t, s, " ", WHITESPACE, 11, 11, 3, 5)
	assertScannerScanned(t, s, "(", LPAREN, 12, 12, 3, 6)
	assertScannerScanned(t, s, "+", SYMBOL, 13, 13, 3, 7)
	assertScannerScanned(t, s, " ", WHITESPACE, 14, 14, 3, 8)
	assertScannerScanned(t, s, "1", NUMBER, 15, 15, 3, 9)
	assertScannerScanned(t, s, " ", WHITESPACE, 16, 16, 3, 10)
	assertScannerScanned(t, s, "-1", NUMBER, 18, 18, 3, 12)
	assertScannerScanned(t, s, ")", RPAREN, 19, 19, 3, 13)
	assertScannerScanned(t, s, " ", WHITESPACE, 20, 20, 3, 14)
	assertScannerScanned(t, s, "0", NUMBER, 21, 21, 3, 15)
	assertScannerScanned(t, s, ")", RPAREN, 22, 22, 3, 16)
	assertScannerScanned(t, s, "\n  ", WHITESPACE, 25, 25, 4, 2)
	assertScannerScanned(t, s, "(", LPAREN, 26, 26, 4, 3)
	assertScannerScanned(t, s, "=", SYMBOL, 27, 27, 4, 4)
	assertScannerScanned(t, s, " ", WHITESPACE, 28, 28, 4, 5)
	assertScannerScanned(t, s, "my-parameter", SYMBOL, 40, 40, 4, 17)
	assertScannerScanned(t, s, " ", WHITESPACE, 41, 41, 4, 18)
	assertScannerScanned(t, s, "fudge sundae", STRING, 55, 55, 4, 32)
	assertScannerScanned(t, s, ")", RPAREN, 56, 56, 4, 33)
	assertScannerScanned(t, s, ")", RPAREN, 57, 57, 4, 34)
	assertScannerScanned(t, s, " ", WHITESPACE, 58, 58, 4, 35)
	assertScannerScanned(t, s, " Crazy", COMMENT, 66, 66, 5, 0)
	assertScannerScanned(t, s, "", EOF, 66, 66, 5, 0)
}
