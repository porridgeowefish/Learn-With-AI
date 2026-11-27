package httpserver

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	"github.com/xmz14/lll/backend-go/internal/modules/assets"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"golang.org/x/net/html"
)

type studyEvidence struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Day   string `json:"day"`
	Text  string `json:"text"`
	URL   string `json:"-"`
}
type studyCitation struct {
	ID    string `json:"id"`
	Quote string `json:"quote"`
}
type studyLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Quote string `json:"quote"`
}

func verifyStudyCitations(citations []studyCitation, evidence []studyEvidence) []studyLink {
	var links []studyLink
	seen := map[string]bool{}
	for _, citation := range citations {
		quote := strings.Join(strings.Fields(citation.Quote), " ")
		if len([]rune(quote)) < 8 || len([]rune(quote)) > 230 || seen[citation.ID] {
			continue
		}
		for _, item := range evidence {
			if item.ID == citation.ID && strings.Contains(item.Text, quote) {
				seen[item.ID] = true
				links = append(links, studyLink{Title: item.Title + " · " + item.Kind + " · " + item.Day, URL: item.URL, Quote: quote})
				break
			}
		}
		if len(links) == 3 {
			break
		}
	}
	return links
}

func studyTerms(text string) map[string]bool {
	out := make(map[string]bool)
	var word strings.Builder
	var previous rune
	flush := func() {
		if word.Len() > 2 {
			out[word.String()] = true
		}
		word.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if unicode.In(r, unicode.Han) {
			flush()
			if previous != 0 {
				out[string([]rune{previous, r})] = true
			}
			previous = r
			continue
		}
		previous = 0
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func studyOverlap(query map[string]bool, text string) int {
	score := 0
	for term := range studyTerms(text) {
		if query[term] {
			score++
		}
	}
	return score
}

func studyExcerpt(text string, query map[string]bool) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	best, score := "", -1
	for offset := 0; offset < len(runes); offset += 210 {
		chunk := string(runes[offset:min(offset+350, len(runes))])
		n := studyOverlap(query, chunk)
		if n > score {
			best, score = chunk, n
		}
	}
	return strings.TrimSpace(best)
}

func reviewPlainText(source string) string {
	z := html.NewTokenizer(strings.NewReader(source))
	z.SetMaxBuf(1024 * 1024)
	var out strings.Builder
	skip := ""
	for out.Len() < 160000 {
		switch z.Next() {
		case html.ErrorToken:
			return out.String()
		case html.StartTagToken:
			name, _ := z.TagName()
			if name := string(name); skip == "" && (name == "style" || name == "script" || name == "head") {
				skip = name
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			if string(name) == skip {
				skip = ""
			}
		case html.TextToken:
			if skip == "" {
				out.Write(z.Text())
				out.WriteByte(' ')
			}
		}
	}
	return out.String()
}

func collectStudyEvidence(ctx context.Context, queryText, focus string, today time.Time) ([]studyEvidence, error) {
	all, err := projects.IndexAll()
	if err != nil {
		return nil, err
	}
	query := studyTerms(queryText)
	monday := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	lastWeek, thisWeek := monday.AddDate(0, 0, -7).Format(time.DateOnly), monday.Format(time.DateOnly)
	weekly := strings.Contains(queryText, "上周") || strings.Contains(queryText, "上星期")
	type candidate struct {
		studyEvidence
		score int
	}
	rank := func(items []candidate) {
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].score != items[j].score {
				return items[i].score > items[j].score
			}
			if items[i].Day != items[j].Day {
				return items[i].Day > items[j].Day
			}
			return items[i].URL < items[j].URL
		})
	}
	var pool []candidate
	for _, project := range all {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if project.ProjectType != projects.ProjectTypeSystemLearning {
			continue
		}
		root, err := projects.ProjectRootForSlug(project.Slug)
		if err != nil {
			continue
		}
		base := "/project/" + url.PathEscape(project.Slug)
		var local []candidate
		add := func(kind, href, text string, date time.Time) {
			day := date.In(today.Location()).Format(time.DateOnly)
			if weekly && (day < lastWeek || day >= thisWeek) {
				return
			}
			excerpt := studyExcerpt(text, query)
			if len([]rune(excerpt)) < 8 {
				return
			}
			score := 2*studyOverlap(query, excerpt) + studyOverlap(query, project.Title)
			if kind == "学习原话" {
				score += 2
			}
			if focus == project.Slug {
				score += 4
			}
			local = append(local, candidate{studyEvidence: studyEvidence{Title: project.Title, Kind: kind, Day: day, Text: excerpt, URL: href}, score: score})
		}
		if stat, err := os.Stat(filepath.Join(root, "conversation", "events.jsonl")); err == nil && stat.Size() > 0 {
			if store, err := teacher.NewConversation(project.Slug); err == nil {
				if messages, err := store.AllMessages(); err == nil {
					for _, item := range messages {
						if item.Status != "completed" || (item.Role != "learner" && item.Role != "teacher") || item.OperationID == "teacher-welcome-v1" {
							continue
						}
						var content strings.Builder
						for _, block := range item.Blocks {
							if block.Type == "markdown" || block.Type == "text" {
								content.WriteString(block.Source)
								content.WriteByte(' ')
							}
						}
						kind := "教师讲解"
						if item.Role == "learner" {
							kind = "学习原话"
						}
						add(kind, base+"/teacher?message="+url.QueryEscape(item.ID), content.String(), item.CreatedAt)
					}
				}
			}
		}
		if stat, err := os.Stat(filepath.Join(root, "assets", "review-html", "asset.json")); err == nil && stat.Mode().IsRegular() {
			if store, err := assets.New(project.Slug); err == nil {
				if asset, err := store.Get("review"); err == nil {
					add("复习稿", base+"/assets?view=review", reviewPlainText(asset.Content), asset.Meta.UpdatedAt)
				}
			}
		}
		rank(local)
		pool = append(pool, local[:min(len(local), 4)]...)
	}
	rank(pool)
	evidence := make([]studyEvidence, 0, min(len(pool), 16))
	for _, item := range pool[:min(len(pool), 16)] {
		item.ID = idgen.New("source")
		evidence = append(evidence, item.studyEvidence)
	}
	return evidence, nil
}
