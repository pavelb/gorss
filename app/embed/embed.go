package embed

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"rss/app/cache"
	"strings"
)

type Embedder struct {
	cache *cache.LRUS
	args  map[string]string
}

type strategy func(string) (string, error)

func NewEmbedder(cache *cache.LRUS, args map[string]string) *Embedder {
	return &Embedder{cache, args}
}

func getURL(url string) (bytes []byte, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
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
	header, ok := resp.Header["Content-Type"];
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
	bytes, err := getURL(u.String())
	if err != nil {
		return
	}
	response := make(map[string]interface{})
	err = json.Unmarshal(bytes, &response)
	if err != nil {
		return
	}

	// Imgur gallery
	if response["provider_name"] == "Imgur" && response["type"] == "rich" {
		bytes, err = getURL(uri)
		if err != nil {
			return
		}
		html := string(bytes)
		const idRegex = "<a name=['\"](\\w+)['\"]"
		matches := regexp.MustCompile(idRegex).FindAllStringSubmatch(html, -1)
		var images []string
		for _, matchGroup := range matches {
			galleryURL := fmt.Sprintf("http://i.imgur.com/%s.png", matchGroup[1])
			images = append(images, e.imageMarkup(galleryURL))
		}
		return fmt.Sprintln(strings.Join(images, "<br/><br/>")), nil
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

func (e *Embedder) addOembedStrategies(strategies *[]strategy) {
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
		*strategies = append(*strategies, func(mustMatch, endpoint string) strategy {
			return func(url string) (markup string, err error) {
				return e.oembed(mustMatch, endpoint, url)
			}
		}(mustMatch, endpoint))
	}
}

func (e *Embedder) embed(url string) string {
	strategies := []strategy{
		e.embedImage,
		e.embedExtensionlessImage,
		e.embedImgurGalleryImage,
	}
	e.addOembedStrategies(&strategies)

	for _, fn := range strategies {
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
