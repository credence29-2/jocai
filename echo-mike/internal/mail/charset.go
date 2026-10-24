package mail

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"
)

// cp1252 maps Windows-1252's 0x80-0x9F range, which is where it differs from
// Latin-1. Zero means unused.
var cp1252 = [32]rune{
	0x20AC, 0, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017D, 0,
	0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0, 0x017E, 0x0178,
}

// DecodeCharset turns a body in the given charset into a Go string. It
// handles UTF-8, ASCII, Latin-1 and Windows-1252, which cover nearly all
// mail. Anything else is read as UTF-8 with bad bytes replaced.
func DecodeCharset(b []byte, charset string) string {
	switch strings.ToLower(strings.Trim(charset, `" `)) {
	case "iso-8859-1", "latin1", "latin-1", "iso8859-1", "windows-1252", "cp1252", "iso-8859-15":
		if utf8.Valid(b) && !bytes.ContainsFunc(b, func(r rune) bool { return r >= 0x80 }) {
			return string(b)
		}
		var sb strings.Builder
		for _, c := range b {
			r := rune(c)
			if c >= 0x80 && c <= 0x9F && cp1252[c-0x80] != 0 {
				r = cp1252[c-0x80]
			}
			sb.WriteRune(r)
		}
		return sb.String()
	}
	return strings.ToValidUTF8(string(b), "�")
}

// CharsetReader lets mime.WordDecoder read headers in the same charsets.
func CharsetReader(charset string, input io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(input)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(DecodeCharset(b, charset)), nil
}
