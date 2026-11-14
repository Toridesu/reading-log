// 日本語をバイト数ではなく文字数で扱い、不正な値を保存前に拒否するテスト。
package books

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input Input
		field string
	}{
		{"empty title", Input{" \t\u3000", "unread", ""}, "title"},
		{"300 Japanese characters", Input{strings.Repeat("本", 300), "reading", ""}, ""},
		{"301 Japanese characters", Input{strings.Repeat("本", 301), "reading", ""}, "title"},
		{"unknown status", Input{"本", "other", ""}, "status"},
		{"10000 emoji", Input{"本", "finished", strings.Repeat("📖", 10000)}, ""},
		{"long notes", Input{"本", "unread", strings.Repeat("あ", 10001)}, "notes"},
		{"invalid title encoding", Input{"\xff", "unread", ""}, "title"},
		{"invalid notes encoding", Input{"本", "unread", "\xff"}, "notes"},
		{"NUL title", Input{"本\x00", "unread", ""}, "title"},
		{"NUL notes", Input{"本", "unread", "\x00"}, "notes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := Validate(tc.input)
			if tc.field == "" && len(problems) != 0 {
				t.Fatal(problems)
			}
			if tc.field != "" && problems[tc.field] == "" {
				t.Fatal("invalid input accepted")
			}
		})
	}
	normalized, problems := Validate(Input{"\u3000 Goの本 \n", "reading", "1行目\r\n2行目"})
	if len(problems) != 0 || normalized.Title != "Goの本" || normalized.Notes != "1行目\n2行目" {
		t.Fatal("normalization failed")
	}
}
