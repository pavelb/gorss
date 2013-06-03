package controllers

import (
	"encoding/json"
	"fmt"
	"github.com/pavelb/gorss/app/cache"
	"github.com/pavelb/gorss/app/embed"
	"github.com/pavelb/gorss/app/rss"
	"github.com/robfig/revel"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
)

type JSONFeed struct {
	Data struct {
		Children []JSONChild
	}
}

type JSONChild struct {
	Data JSONItem
}

type JSONItem struct {
	Url         string
	Title       string
	Description string
	Created_utc float64
	Permalink   string
	Over_18     bool
}

func getURL(url string) (bytes []byte, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func newRSSItem(jsonItem *JSONItem, embedder *embed.Embedder) *rss.Item {
	comments := "http://reddit.com" + jsonItem.Permalink
	description := fmt.Sprintf(
		"%s<br/><br/><a href='%s'>Comments</a>",
		embedder.Embed(jsonItem.Url),
		comments,
	)
	return &rss.Item{
		Title:       jsonItem.Title,
		Link:        jsonItem.Url,
		Description: description,
		Comments:    comments,
		GUID:        jsonItem.Url,
		PubDate:     time.Unix(int64(jsonItem.Created_utc), 0).Format(time.RFC822),
	}
}

func newJSONFeed(subreddit string, limit int) (jsonFeed *JSONFeed, err error) {
	bytes, err := getURL(fmt.Sprintf("http://www.reddit.com/r/%s.json?limit=%d", subreddit, limit))
	if err != nil {
		return
	}
	jsonFeed = new(JSONFeed)
	err = json.Unmarshal(bytes, jsonFeed)
	return
}

func (j *JSONFeed) filterNSFW() {
	var children []JSONChild
	for _, child := range j.Data.Children {
		if !child.Data.Over_18 {
			children = append(children, child)
		}
	}
	j.Data.Children = children
}

func newRSSFeed(subreddit string, limit int, embedder *embed.Embedder) (feed *rss.Feed, err error) {
	jsonFeed, err := newJSONFeed(subreddit, limit)
	if err != nil {
		return
	}
	jsonFeed.filterNSFW()

	feed = &rss.Feed{
		Version: 2.0,
		Channel: rss.Channel{
			Title:       "r/" + subreddit,
			Link:        "http://www.reddit.com/r/" + subreddit,
			Description: "Embelished version of 'r/" + subreddit + "' subreddit RSS feed",
			TTL:         10,
			Items:       make([]*rss.Item, len(jsonFeed.Data.Children)),
		},
	}

	var wg sync.WaitGroup
	for i := range jsonFeed.Data.Children {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			feed.Channel.Items[i] = newRSSItem(&jsonFeed.Data.Children[i].Data, embedder)
		}(i)
	}
	wg.Wait()
	return
}

type Reddit struct {
	*revel.Controller
}

type Html string

func (r Html) Apply(req *revel.Request, resp *revel.Response) {
	resp.WriteHeader(http.StatusOK, "text/html")
	resp.Out.Write([]byte(r))
}

func (c Reddit) Feed(r string) revel.Result {
	const embedCacheFile = "embedCache"
	cache, err := cache.LoadLRUS(100*1024, embedCacheFile)
	if err != nil {
		return Html(fmt.Sprint(err))
	}
	embedder := embed.NewEmbedder(cache, map[string]string{
		"EmbedlyAPIKey": "8b02b918d50e4e33b9152d62985d6241",
		"maxWidth":      "768",
	})
	feed, err := newRSSFeed(r, 100, embedder)
	if err != nil {
		return Html(fmt.Sprint(err))
	}
	err = cache.Save(embedCacheFile)
	if err != nil {
		return Html(fmt.Sprint(err))
	}
	return c.RenderXml(feed)
}
