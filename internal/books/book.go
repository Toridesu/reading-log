// 読書タスクの項目と入力ルール。画面から送られた所有者IDは受け取らない。
package books

import (
	"strings"
	"time"
	"unicode/utf8"
)

type Input struct{ Title, Status, Notes string }
type Book struct {
	ID                   int64
	Title, Status, Notes string
	CreatedAt, UpdatedAt time.Time
}

func (b Book) StatusLabel() string {
	switch b.Status {
	case "reading":
		return "読書中"
	case "finished":
		return "読了"
	default:
		return "未読"
	}
}

// Validateは書名の余分な空白を除き、DBへ渡す前に文字数と状態を確認する。
func Validate(input Input) (Input, map[string]string) {
	input.Title = strings.TrimSpace(input.Title)
	input.Notes = strings.ReplaceAll(input.Notes, "\r\n", "\n")
	problems := map[string]string{}
	if !utf8.ValidString(input.Title) || strings.ContainsRune(input.Title, 0) || utf8.RuneCountInString(input.Title) < 1 || utf8.RuneCountInString(input.Title) > 300 {
		problems["title"] = "書名は1〜300文字で入力してください。"
	}
	if input.Status != "unread" && input.Status != "reading" && input.Status != "finished" {
		problems["status"] = "未読・読書中・読了から選んでください。"
	}
	if !utf8.ValidString(input.Notes) || strings.ContainsRune(input.Notes, 0) || utf8.RuneCountInString(input.Notes) > 10000 {
		problems["notes"] = "メモは10,000文字以内で入力してください。"
	}
	return input, problems
}
