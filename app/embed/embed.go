package embed

import (
	"code.google.com/p/go.net/html"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type stringCache interface {
	Set(string, string)
	Get(string) (string, bool)
}

type Embedder struct {
	cache stringCache
	args  map[string]string
}

type EmbedInfo struct {
	URL  string
	Html string
}

type strategy func(string) (EmbedInfo, error)

var strategyWhiffError error = errors.New("No matching strategy.")

func (e *Embedder) strategyRunner(url string, strategies []strategy) (rv EmbedInfo, err error) {
	for _, fn := range strategies {
		rv, err = fn(url)
		if err == nil {
			return
		} else if err != strategyWhiffError {
			fmt.Sprintln("Error during getMarkup('%s'): %s", url, err)
		}
	}
	err = strategyWhiffError
	return
}

func NewEmbedder(cache stringCache, args map[string]string) *Embedder {
	return &Embedder{cache: cache, args: args}
}

func getBytes(url string) (bytes []byte, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func getHTML(url string) (html string, err error) {
	bytes, err := getBytes(url)
	if err == nil {
		html = string(bytes)
	}
	return
}

func imageMarkup(url string) string {
	return fmt.Sprintf("<img style='max-width:100%%' src='%s'/>", url)
}

func imgurGalleryMarkup(url string) (markup string, err error) {
	html, err := getHTML(url)
	if err != nil {
		return
	}
	const idRegex = "<a name=['\"](\\w+)['\"]"
	matches := regexp.MustCompile(idRegex).FindAllStringSubmatch(html, -1)
	var partials []string
	for _, matchGroup := range matches {
		if len(matchGroup) > 1 {
			imgURL := "http://i.imgur.com/" + matchGroup[1] + ".png"
			partials = append(partials, imageMarkup(imgURL))
		}
	}
	markup = strings.Join(partials, "<br/><br/>")
	return
}

func (e *Embedder) embedSimpleImage(url string) (rv EmbedInfo, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if header, ok := resp.Header["Content-Type"]; ok {
		for _, mimeType := range header {
			if strings.HasPrefix(mimeType, "image") {
				rv.URL = url
				rv.Html = imageMarkup(url)
				return
			}
		}
	}
	err = strategyWhiffError
	return
}

func (e *Embedder) embedExtensionlessImage(url string) (EmbedInfo, error) {
	return e.embedSimpleImage(strings.TrimRight(url, "/") + ".png")
}

func (e *Embedder) embedImgurGalleryImage(url string) (EmbedInfo, error) {
	return e.embedExtensionlessImage(strings.Replace(url, "/gallery", "", 1))
}

func (e *Embedder) embedQuickmeme(url string) (rv EmbedInfo, err error) {
	matched, err := regexp.MatchString("(quickmeme.com|qkme.me)", url)
	if err != nil {
		return
	}
	if !matched {
		err = strategyWhiffError
		return
	}
	html, err := getHTML(url)
	if err != nil {
		return
	}
	const idRegex = "id=\"img\".*src=\"(.*)\""
	matchGroup := regexp.MustCompile(idRegex).FindStringSubmatch(html)
	if len(matchGroup) > 1 {
		rv.URL = matchGroup[1]
		rv.Html = imageMarkup(rv.URL)
		return
	}
	err = strategyWhiffError
	return
}

func (e *Embedder) embedImage(url string) (rv EmbedInfo, err error) {
	return e.strategyRunner(url, []strategy{
		e.embedSimpleImage,
		e.embedExtensionlessImage,
		e.embedImgurGalleryImage,
		e.embedQuickmeme,
	})
}

func (e *Embedder) embedRedditSelf(url string) (rv EmbedInfo, err error) {
	matched, err := regexp.MatchString("reddit.com/r/", url)
	if err != nil {
		return
	}
	if !matched {
		err = strategyWhiffError
		return
	}
	rv.URL = url
	doc, err := goquery.NewDocument(url)
	if err != nil {
		return
	}
	doc.Find(".expando .usertext-body").Each(func(i int, s *goquery.Selection) {
		s.Find("a").Each(func(i int, s *goquery.Selection) {
			if href, ok := s.Attr("href"); ok {
				embedInfo, err := e.embedImage(href)
				if err != nil {
					return
				}
				node, err := html.Parse(strings.NewReader(embedInfo.Html))
				if err != nil {
					return
				}
				parent := s.Parent().Get(0)
				parent.RemoveChild(s.Get(0))
				parent.AppendChild(node)
			}
		})
		rv.Html, err = s.Html()
		return
	})
	return
}

func (e *Embedder) oembed(mustMatch, endpoint, uri string) (
	rv EmbedInfo, err error) {

	matched, err := regexp.MatchString(mustMatch, uri)
	if err != nil {
		return
	}
	if !matched {
		err = strategyWhiffError
		return
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return
	}
	q := u.Query()
	q.Set("format", "json")
	q.Set("url", uri)
	if maxWidth, ok := e.args["maxWidth"]; ok {
		q.Set("maxwidth", maxWidth)
	}
	u.RawQuery = q.Encode()
	bytes, err := getBytes(u.String())
	if err != nil {
		return
	}
	response := make(map[string]interface{})
	err = json.Unmarshal(bytes, &response)
	if err != nil {
		return
	}

	if resURL, ok := response["url"]; ok {
		rv.URL = resURL.(string)
	} else {
		rv.URL = uri
	}

	if response["provider_name"] == "Imgur" && response["type"] == "rich" {
		rv.Html, err = imgurGalleryMarkup(uri)
	} else if html, ok := response["html"]; ok {
		// strip "http:" to force relative protocol (hacky)
		rv.Html = strings.Replace(html.(string), "http:", "", -1)
	} else if response["type"] == "photo" {
		rv.Html = imageMarkup(response["url"].(string))
	} else if description, ok := response["description"]; ok {
		rv.Html = description.(string)
	} else {
		err = strategyWhiffError
	}
	return
}

func (e *Embedder) embedOembed(url string) (rv EmbedInfo, err error) {
	providers := map[string]string{
		"http://api.imgur.com/oembed":             "imgur",
		"http://www.youtube.com/oembed":           "youtu",
		"http://www.flickr.com/services/oembed":   "flic",
		"http://lab.viddler.com/services/oembed":  "viddler",
		"http://qik.com/api/oembed.json":          "qik",
		"http://revision3.com/api/oembed":         "revision3",
		"http://www.hulu.com/api/oembed.json":     "hulu",
		"http://vimeo.com/api/oembed.json":        "vimeo",
		"http://www.collegehumor.com/oembed.json": "collegehumor",
	}
	if apiKey, ok := e.args["EmbedlyAPIKey"]; ok {
		providers["http://api.embed.ly/1/oembed?key="+apiKey] = ""
	}
	strategies := make([]strategy, 0)
	for endpoint, mustMatch := range providers {
		strategies = append(strategies, func(mustMatch, endpoint string) strategy {
			return func(url string) (rv EmbedInfo, err error) {
				return e.oembed(mustMatch, endpoint, url)
			}
		}(mustMatch, endpoint))
	}
	return e.strategyRunner(url, strategies)
}

func (e *Embedder) embed(url string) (rv EmbedInfo, err error) {
	return e.strategyRunner(url, []strategy{
		e.embedRedditSelf,
		e.embedImage,
		e.embedOembed,
	})
}

func (e *Embedder) Embed(url string) (rv EmbedInfo, err error) {
	bytes, err := json.Marshal(e.args)
	if err != nil {
		return
	}
	key := url + string(bytes)
	pack, cacheHit := e.cache.Get(key)
	if cacheHit {
		// oof, what a hack
		pieces := strings.SplitN(pack, " ", 2)
		if len(pieces) == 2 {
			rv.URL = pieces[0]
			rv.Html = pieces[1]
			return
		}
	}
	rv, err = e.embed(url)
	if err != nil {
		return
	}
	pack = rv.URL + " " + rv.Html
	e.cache.Set(key, pack)
	return
}
