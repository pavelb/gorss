package embed

import (
	"encoding/json"
	"fmt"
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
	cache      stringCache
	args       map[string]string
	strategies []strategy
}

type strategy func(string) (string, error)

func NewEmbedder(cache stringCache, args map[string]string) *Embedder {
	e := &Embedder{cache: cache, args: args}
	e.strategies = []strategy{
		e.embedImage,
		e.embedExtensionlessImage,
		e.embedImgurGalleryImage,
		e.embedQuickmeme,
	}
	e.addOembedStrategies()
	return e
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
	if err != nil {
		return
	}
	return string(bytes), nil
}

func (e *Embedder) imageMarkup(url string) string {
	return fmt.Sprintf("<img style='max-width:100%%' src='%s'/>", url)
}

func (e *Embedder) embedImage(url string) (markup string, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	header, ok := resp.Header["Content-Type"]
	if !ok {
		return
	}
	for _, mimeType := range header {
		const prefix = "image"
		if len(mimeType) >= len(prefix) && mimeType[:len(prefix)] == prefix {
			return e.imageMarkup(url), nil
		}
	}
	return
}

func (e *Embedder) embedExtensionlessImage(url string) (markup string, err error) {
	return e.embedImage(strings.TrimRight(url, "/") + ".png")
}

func (e *Embedder) embedImgurGalleryImage(url string) (markup string, err error) {
	return e.embedExtensionlessImage(strings.Replace(url, "/gallery", "", 1))
}

func (e *Embedder) embedImgurGallery(url string) (markup string, err error) {
	html, err := getHTML(url)
	if err != nil {
		return
	}
	const idRegex = "<div class=\"image\" id=\"(\\w+)\">[^>]*>([^<]*)<"
	matches := regexp.MustCompile(idRegex).FindAllStringSubmatch(html, -1)
	var partials []string
	for _, matchGroup := range matches {
		if len(matchGroup) < 3 {
			continue
		}
		imageID := matchGroup[1]
		heading := matchGroup[2]
		imageURL := fmt.Sprintf("http://i.imgur.com/%s.png", imageID)
		partialMarkup := fmt.Sprintf("%s<br/><br/>%s", heading, e.imageMarkup(imageURL))
		partials = append(partials, partialMarkup)
	}
	return strings.Join(partials, "<br/><br/>"), nil
}

func (e *Embedder) embedQuickmeme(url string) (markup string, err error) {
	matched, err := regexp.MatchString("quickmeme.com", url)
	if err != nil || !matched {
		return
	}
	html, err := getHTML(url)
	if err != nil {
		return
	}
	const idRegex = "id=\"img\".*src=\"(.*)\""
	matchGroup := regexp.MustCompile(idRegex).FindStringSubmatch(html)
	if len(matchGroup) > 1 {
		markup = e.imageMarkup(matchGroup[1])
	}
	return
}

func (e *Embedder) oembed(mustMatch, endpoint, uri string) (markup string, err error) {
	matched, err := regexp.MatchString(mustMatch, uri)
	if err != nil || !matched {
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

	if response["provider_name"] == "Imgur" && response["type"] == "rich" {
		return e.embedImgurGallery(uri)
	}

	if html, ok := response["html"]; ok {
		return strings.Replace(html.(string), "http:", "", -1), nil
	}
	if resType, ok := response["type"]; ok {
		switch resType {
		case "photo":
			if resUrl, ok := response["url"]; ok {
				return e.imageMarkup(resUrl.(string)), nil
			}
		case "link":
			if description, ok := response["description"]; ok {
				return description.(string), nil
			}
		}
	}
	return
}

func (e *Embedder) addOembedStrategies() {
	providers := map[string]string{
		"http://api.imgur.com/oembed":             "imgur",
		"http://www.youtube.com/oembed":           "youtu",
		"http://www.flickr.com/services/oembed":   "flickr",
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
	for endpoint, mustMatch := range providers {
		e.strategies = append(e.strategies, func(mustMatch, endpoint string) strategy {
			return func(url string) (markup string, err error) {
				return e.oembed(mustMatch, endpoint, url)
			}
		}(mustMatch, endpoint))
	}
}

func (e *Embedder) embed(url string) string {
	for _, fn := range e.strategies {
		markup, err := fn(url)
		if err != nil {
			fmt.Sprintln("Error during getMarkup('%s'): %s", url, err)
		} else if markup != "" {
			return markup
		}
	}
	return "..."
}

func (e *Embedder) Embed(url string) string {
	bytes, err := json.Marshal(e.args)
	if err != nil {
		return fmt.Sprint(err)
	}
	key := url + string(bytes)
	if description, ok := e.cache.Get(key); ok {
		return description
	}
	description := e.embed(url)
	e.cache.Set(key, description)
	return description
}
