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

// Check out http://noembed.com/

type Strategy func(string, *map[string]string) (string, error)

func getURL(url string) (bytes []byte, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func imageMarkup(url string) string {
	return fmt.Sprintf("<img style='max-width:100%%' src='%s'/>", url)
}

func embedImage(url string, args *map[string]string) (markup string, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if header, ok := resp.Header["Content-Type"]; ok {
		for _, mimeType := range header {
			const prefix = "image"
			if len(mimeType) >= len(prefix) && mimeType[:len(prefix)] == prefix {
				return imageMarkup(url), nil
			}
		}
	}
	return
}

func embedExtensionlessImage(url string, args *map[string]string) (markup string, err error) {
	return embedImage(strings.TrimRight(url, "/")+".png", args)
}

func embedImgurGalleryImage(url string, args *map[string]string) (markup string, err error) {
	return embedExtensionlessImage(strings.Replace(url, "/gallery", "", 1), args)
}

func oembed(mustMatch string, endpoint string, uri string, args *map[string]string) (markup string, err error) {
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
	if maxWidth, ok := (*args)["maxWidth"]; ok {
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
			images = append(images, imageMarkup(galleryURL))
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
				return imageMarkup(resUrl.(string)), nil
			}
		case "link":
			if description, ok := response["description"]; ok {
				return description.(string), nil
			}
		}
	}
	return
}

func addOembedStrategies(strategies *[]Strategy, args *map[string]string) {
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
	if apiKey, ok := (*args)["EmbedlyAPIKey"]; ok {
		providers["http://api.embed.ly/1/oembed?key="+apiKey] = ""
	}
	for endpoint, mustMatch := range providers {
		*strategies = append(*strategies, func(mustMatch string, endpoint string) Strategy {
			return func(url string, args *map[string]string) (markup string, err error) {
				return oembed(mustMatch, endpoint, url, args)
			}
		}(mustMatch, endpoint))
	}
}

func getMarkup(url string, args *map[string]string) string {
	strategies := []Strategy{
		embedImage,
		embedExtensionlessImage,
		embedImgurGalleryImage,
	}
	addOembedStrategies(&strategies, args)

	for _, strategy := range strategies {
		markup, err := strategy(url, args)
		if err != nil {
			fmt.Sprintln("Error during getMarkup('%s'): %s", url, err)
		} else if markup != "" {
			return markup
		}
	}
	return "..."
}

func GetMarkup(url string, args *map[string]string, cache *cache.LRUS) string {
	bytes, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprint(err)
	}
	key := url + string(bytes)
	if description, ok := cache.Get(key); ok {
		return description
	}
	description := getMarkup(url, args)
	cache.Set(key, description)
	return description
}
