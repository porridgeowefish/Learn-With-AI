// Package visuals retrieves bounded, attributed teaching figures from public
// providers. It does not accept arbitrary hosts or send credentials/documents.
package visuals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/platform/netproxy"
)

const commonsAPI = "https://commons.wikimedia.org/w/api.php"
const pubchemAPI = "https://pubchem.ncbi.nlm.nih.gov/rest/pug"
const maxImageBytes = 3 << 20

var client = &http.Client{Transport: netproxy.Transport(), Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New("visual redirects are not allowed") }}
var tags = regexp.MustCompile(`<[^>]*>`)
var commonsAsset = regexp.MustCompile(`^[a-f0-9]/[a-f0-9]{2}/[^/\\?#]+\.svg$`)
var compoundID = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)

type Figure struct {
	Provider   string `json:"provider"`
	Asset      string `json:"asset"`
	Title      string `json:"title"`
	SourceURL  string `json:"sourceUrl"`
	Author     string `json:"author"`
	License    string `json:"license"`
	LicenseURL string `json:"licenseUrl,omitempty"`
}

// Search returns at most three SVG candidates or one exact chemical structure.
// The full images never enter the model context.
func Search(ctx context.Context, kind, query string) ([]Figure, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > 120 {
		return nil, errors.New("query must contain 1–120 characters")
	}
	switch kind {
	case "diagram":
		return searchCommons(ctx, query)
	case "molecule":
		return searchCompound(ctx, query)
	default:
		return nil, errors.New("kind must be diagram or molecule")
	}
}

func searchCommons(ctx context.Context, query string) ([]Figure, error) {
	params := url.Values{"action": {"query"}, "format": {"json"}, "generator": {"search"}, "gsrsearch": {query + " filetype:drawing"}, "gsrnamespace": {"6"}, "gsrlimit": {"8"}, "prop": {"imageinfo"}, "iiprop": {"url|mime|extmetadata"}, "iiextmetadatafilter": {"Artist|LicenseShortName|LicenseUrl|Restrictions"}}
	return commonsFigures(ctx, params)
}

func commonsFigures(ctx context.Context, params url.Values) ([]Figure, error) {
	return commonsFiguresLimit(ctx, params, 3)
}

func commonsFiguresLimit(ctx context.Context, params url.Values, limit int) ([]Figure, error) {
	var payload struct {
		Query struct {
			Pages map[string]struct {
				Title     string `json:"title"`
				Index     int    `json:"index"`
				ImageInfo []struct {
					URL            string `json:"url"`
					DescriptionURL string `json:"descriptionurl"`
					MIME           string `json:"mime"`
					Metadata       map[string]struct {
						Value string `json:"value"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := getJSON(ctx, commonsAPI+"?"+params.Encode(), &payload); err != nil {
		return nil, err
	}
	if payload.Error != nil {
		return nil, fmt.Errorf("Commons search: %s", payload.Error.Code)
	}
	type ranked struct {
		index  int
		figure Figure
	}
	var candidates []ranked
	for _, page := range payload.Query.Pages {
		if len(page.ImageInfo) == 0 {
			continue
		}
		info := page.ImageInfo[0]
		u, err := url.Parse(info.URL)
		if err != nil || u.Scheme != "https" || u.Host != "upload.wikimedia.org" || u.User != nil || u.Fragment != "" || info.MIME != "image/svg+xml" {
			continue
		}
		asset := strings.TrimPrefix(u.Path, "/wikipedia/commons/")
		license := plain(info.Metadata["LicenseShortName"].Value, 100)
		author := plain(info.Metadata["Artist"].Value, 240)
		if !commonsAsset.MatchString(asset) || !openLicense(license) || strings.TrimSpace(info.Metadata["Restrictions"].Value) != "" {
			continue
		}
		if author == "" {
			if strings.EqualFold(license, "Public domain") || strings.HasPrefix(strings.ToLower(license), "cc0") {
				author = "作者未标注"
			} else {
				continue
			}
		}
		source := info.DescriptionURL
		if !safeURL(source, "commons.wikimedia.org") {
			continue
		}
		licenseURL := info.Metadata["LicenseUrl"].Value
		if !safeURL(licenseURL, "creativecommons.org") && !safeURL(licenseURL, "opensource.org") {
			licenseURL = ""
		}
		candidates = append(candidates, ranked{page.Index, Figure{Provider: "commons", Asset: asset, Title: plain(strings.TrimPrefix(page.Title, "File:"), 200), SourceURL: source, Author: author, License: license, LicenseURL: licenseURL}})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].index == candidates[j].index {
			return candidates[i].figure.Asset < candidates[j].figure.Asset
		}
		return candidates[i].index < candidates[j].index
	})
	result := make([]Figure, 0, limit)
	for _, candidate := range candidates {
		rememberFigure(candidate.figure)
		result = append(result, candidate.figure)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func searchCompound(ctx context.Context, query string) ([]Figure, error) {
	endpoint := pubchemAPI + "/compound/name/" + url.PathEscape(query) + "/cids/JSON"
	var payload struct {
		IdentifierList struct {
			CID []int64 `json:"CID"`
		} `json:"IdentifierList"`
	}
	if err := getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	if len(payload.IdentifierList.CID) == 0 {
		return []Figure{}, nil
	}
	id := strconv.FormatInt(payload.IdentifierList.CID[0], 10)
	if !compoundID.MatchString(id) {
		return nil, errors.New("invalid PubChem identifier")
	}
	figure := Figure{Provider: "pubchem", Asset: id, Title: plain(query, 120) + " · CID " + id, SourceURL: "https://pubchem.ncbi.nlm.nih.gov/compound/" + id, Author: "PubChem / NCBI", License: "PubChem 结构图；来源数据的权利说明见原始记录"}
	rememberFigure(figure)
	return []Figure{figure}, nil
}

type figureEntry struct {
	figure  Figure
	created time.Time
}

var figureCache = struct {
	sync.Mutex
	entries map[string]figureEntry
}{entries: map[string]figureEntry{}}

func rememberFigure(figure Figure) {
	figureCache.Lock()
	defer figureCache.Unlock()
	if len(figureCache.entries) >= 256 {
		var oldest string
		var when time.Time
		for key, v := range figureCache.entries {
			if oldest == "" || v.created.Before(when) {
				oldest = key
				when = v.created
			}
		}
		delete(figureCache.entries, oldest)
	}
	figureCache.entries[figure.Provider+":"+figure.Asset] = figureEntry{figure, time.Now()}
}

// Metadata revalidates the resource against the provider, so model-produced
// attribution or license fields are never the source of truth in the UI.
func Metadata(ctx context.Context, provider, asset string) (Figure, error) {
	if !validAsset(provider, asset) {
		return Figure{}, errors.New("invalid figure identifier")
	}
	figureCache.Lock()
	entry, ok := figureCache.entries[provider+":"+asset]
	figureCache.Unlock()
	if ok && time.Since(entry.created) < time.Hour {
		return entry.figure, nil
	}
	if provider == "commons" {
		parts := strings.Split(asset, "/")
		params := url.Values{"action": {"query"}, "format": {"json"}, "titles": {"File:" + parts[2]}, "prop": {"imageinfo"}, "iiprop": {"url|mime|extmetadata"}, "iiextmetadatafilter": {"Artist|LicenseShortName|LicenseUrl|Restrictions"}}
		figures, err := commonsFigures(ctx, params)
		if err != nil {
			return Figure{}, err
		}
		for _, figure := range figures {
			if figure.Asset == asset {
				return figure, nil
			}
		}
		return Figure{}, errors.New("figure attribution could not be verified")
	}
	var payload struct {
		PropertyTable struct {
			Properties []struct {
				CID   int64  `json:"CID"`
				Title string `json:"Title"`
			} `json:"Properties"`
		} `json:"PropertyTable"`
	}
	if err := getJSON(ctx, pubchemAPI+"/compound/cid/"+asset+"/property/Title/JSON", &payload); err != nil {
		return Figure{}, err
	}
	if len(payload.PropertyTable.Properties) == 0 || strconv.FormatInt(payload.PropertyTable.Properties[0].CID, 10) != asset {
		return Figure{}, errors.New("compound metadata could not be verified")
	}
	figure := Figure{Provider: "pubchem", Asset: asset, Title: plain(payload.PropertyTable.Properties[0].Title, 200) + " · CID " + asset, SourceURL: "https://pubchem.ncbi.nlm.nih.gov/compound/" + asset, Author: "PubChem / NCBI", License: "PubChem 结构图；来源数据的权利说明见原始记录"}
	rememberFigure(figure)
	return figure, nil
}
func validAsset(provider, asset string) bool {
	return (provider == "commons" && len(asset) < 600 && commonsAsset.MatchString(asset)) || (provider == "pubchem" && compoundID.MatchString(asset))
}

func plain(s string, limit int) string {
	s = strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(s, ""))), " ")
	r := []rune(s)
	if len(r) > limit {
		s = string(r[:limit])
	}
	return s
}
func safeURL(raw, host string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host == host && u.User == nil
}
func openLicense(value string) bool {
	s := strings.ToLower(value)
	return s == "public domain" || strings.HasPrefix(s, "cc0") || strings.HasPrefix(s, "cc by ") || strings.HasPrefix(s, "cc by-sa ") || s == "mit"
}
func getJSON(ctx context.Context, endpoint string, target any) error {
	data, _, err := fetch(ctx, endpoint, 1<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
func fetch(ctx context.Context, endpoint string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "LearnWithAI/0.8 (educational figures; https://github.com/xmz14/learn-with-ai)")
	res, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("figure provider returned HTTP %d", res.StatusCode)
	}
	if res.ContentLength > limit {
		return nil, "", errors.New("figure response is too large")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", errors.New("figure response is too large")
	}
	return data, res.Header.Get("Content-Type"), nil
}

type imageEntry struct {
	data    []byte
	mime    string
	created time.Time
}

var imageCache = struct {
	sync.Mutex
	entries map[string]imageEntry
}{entries: map[string]imageEntry{}}

// Image uses two fixed upstream hosts and canonical provider IDs. Cache entries
// are bounded (24 * 3 MiB), have a one-hour lifetime, and contain no user files.
func Image(ctx context.Context, provider, asset string) ([]byte, string, error) {
	var endpoint, mime string
	switch {
	case provider == "commons" && len(asset) < 600 && commonsAsset.MatchString(asset):
		u := url.URL{Scheme: "https", Host: "upload.wikimedia.org", Path: "/wikipedia/commons/" + asset}
		endpoint = u.String()
		mime = "image/svg+xml"
	case provider == "pubchem" && compoundID.MatchString(asset):
		endpoint = pubchemAPI + "/compound/cid/" + asset + "/PNG?image_size=large"
		mime = "image/png"
	default:
		return nil, "", errors.New("invalid figure identifier")
	}
	imageCache.Lock()
	entry, ok := imageCache.entries[endpoint]
	imageCache.Unlock()
	if ok && time.Since(entry.created) < time.Hour {
		return entry.data, entry.mime, nil
	}
	data, reported, err := fetch(ctx, endpoint, maxImageBytes)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(strings.Split(reported, ";")[0]) != mime {
		return nil, "", errors.New("unexpected figure content type")
	}
	if mime == "image/png" && http.DetectContentType(data) != "image/png" {
		return nil, "", errors.New("invalid PNG figure")
	}
	if mime == "image/svg+xml" && !strings.Contains(string(data), "<svg") {
		return nil, "", errors.New("invalid SVG figure")
	}
	imageCache.Lock()
	defer imageCache.Unlock()
	if len(imageCache.entries) >= 24 {
		var oldest string
		var when time.Time
		for key, v := range imageCache.entries {
			if oldest == "" || v.created.Before(when) {
				oldest = key
				when = v.created
			}
		}
		delete(imageCache.entries, oldest)
	}
	imageCache.entries[endpoint] = imageEntry{data: data, mime: mime, created: time.Now()}
	return data, mime, nil
}
