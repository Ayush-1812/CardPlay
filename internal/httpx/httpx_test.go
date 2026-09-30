package httpx

import (
	"strings"
	"testing"
)

func TestCleanText(t *testing.T) {
	cases := []struct {
		in, out string
		ok      bool
	}{
		{"  hello  ", "hello", true},
		{"héllo 👋 ♠", "héllo 👋 ♠", true},
		{"", "", false},
		{"   ", "", false},
		{"a\nb", "", false},
		{"a\tb", "", false},
		{"a\x00b", "", false},
		{"a\x7fb", "", false},
		{"a\u0085b", "", false},
		{"left‮right", "", false},
		{"iso⁦late", "", false},
		{"bad\xffutf8", "", false},
		{strings.Repeat("é", 10), strings.Repeat("é", 10), true},
		{strings.Repeat("é", 11), "", false},
	}
	for _, c := range cases {
		got, ok := CleanText(c.in, 10)
		if ok != c.ok || (ok && got != c.out) {
			t.Errorf("CleanText(%q)=%q,%v want %q,%v", c.in, got, ok, c.out, c.ok)
		}
	}
}
