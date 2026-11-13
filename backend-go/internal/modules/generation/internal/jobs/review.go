package generation

import (
	"errors"
	"io"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"golang.org/x/net/html"
)

func generationSearcher(kind string) teacher.WebSearcher {
	if kind == "review" {
		return teacher.ConfiguredYuanbaoSearch()
	}
	return teacher.ConfiguredWebSearch()
}

func reviewHTML(raw string) (string, error) {
	body := strings.TrimSpace(raw)
	if opening, tail, ok := strings.Cut(body, "\n"); ok &&
		strings.EqualFold(strings.TrimSpace(opening), "```html") && strings.HasSuffix(tail, "```") {
		body = strings.TrimSpace(strings.TrimSuffix(tail, "```"))
	}
	invalid := errors.New("review must contain one complete HTML article")
	tokens := html.NewTokenizer(strings.NewReader(body))
	if tokens.Next() != html.StartTagToken || tokens.Token().Data != "article" {
		return "", invalid
	}
	closed, hasText := false, false
	for {
		switch tokens.Next() {
		case html.ErrorToken:
			if tokens.Err() != io.EOF || !closed || !hasText {
				return "", invalid
			}
			return body, nil
		case html.TextToken:
			if strings.TrimSpace(string(tokens.Text())) != "" {
				if closed {
					return "", invalid
				}
				hasText = true
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			tag := tokens.Token().Data
			if closed || tag == "article" {
				return "", invalid
			}
			switch tag {
			case "script", "style", "iframe", "object", "embed", "template":
				return "", invalid
			}
		case html.EndTagToken:
			if closed {
				return "", invalid
			}
			closed = tokens.Token().Data == "article"
		case html.DoctypeToken:
			return "", invalid
		}
	}
}
