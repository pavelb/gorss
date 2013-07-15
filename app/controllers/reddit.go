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

type redditJSONFeed struct {
	Data struct {
		Children []redditJSONChild
	}
}

type redditJSONChild struct {
	Data redditJSONItem
}

type redditJSONItem struct {
	URL         string
	Title       string
	Description string
	Created_utc float64
	Permalink   string
	Over_18     bool
}

type stringCache interface {
	Set(string, string)
	Get(string) (string, bool)
}

func getURL(url string) (bytes []byte, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func newRSSItem(
	jsonItem *redditJSONItem,
	repostCache stringCache,
	embedder *embed.Embedder,
) (item *rss.Item) {

	if jsonItem.Over_18 {
		return nil
	}

	comments := "http://reddit.com" + jsonItem.Permalink

	item = &rss.Item{
		Title:       jsonItem.Title,
		Link:        jsonItem.URL,
		Description: fmt.Sprintf(
			"%s<br/><br/><a href='%s'>Comments</a>",
			embedder.Embed(jsonItem.URL),
			comments,
		),
		Comments:    comments,
		GUID:        jsonItem.URL,
		PubDate:     time.Unix(int64(jsonItem.Created_utc), 0).Format(time.RFC822),
	}

	if permalink, ok := repostCache.Get(jsonItem.URL); ok && permalink != jsonItem.Permalink {
		item.Title += " (Repost)"
		return
	}

	repostCache.Set(jsonItem.URL, jsonItem.Permalink)
	return
}

func getRedditJSONFeed(subreddit string, limit int) (jsonFeed *redditJSONFeed, err error) {
	bytes, err := getURL(fmt.Sprintf("http://www.reddit.com/r/%s.json?limit=%d", subreddit, limit))
	if err != nil {
		return
	}
	jsonFeed = new(redditJSONFeed)
	err = json.Unmarshal(bytes, jsonFeed)
	return
}

func getRedditXMLFeed(
	subreddit string,
	limit int,
	repostCache stringCache,
	embedder *embed.Embedder,
) (feed *rss.Feed, err error) {

	jsonFeed, err := getRedditJSONFeed(subreddit, limit)
	if err != nil {
		return
	}

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
			feed.Channel.Items[i] = newRSSItem(
				&jsonFeed.Data.Children[i].Data,
				repostCache,
				embedder,
			)
		}(i)
	}
	wg.Wait()
	return
}

type Reddit struct {
	*revel.Controller
}

type HTML string

func (r HTML) Apply(req *revel.Request, resp *revel.Response) {
	resp.WriteHeader(http.StatusOK, "text/html")
	resp.Out.Write([]byte(r))
}

func (c Reddit) Feed(r string) revel.Result {
	const repostCacheFile = "repostCache"
	const embedCacheFile = "embedCache"

	repostCache, err := cache.LoadLRUS(100*1024, repostCacheFile)
	if err != nil {
		return HTML(fmt.Sprint(err))
	}

	embedCache, err := cache.LoadLRUS(100*1024, embedCacheFile)
	if err != nil {
		return HTML(fmt.Sprint(err))
	}
	embedder := embed.NewEmbedder(embedCache, map[string]string{
		"EmbedlyAPIKey": "8b02b918d50e4e33b9152d62985d6241",
		"maxWidth":      "768",
	})

	feed, err := getRedditXMLFeed(r, 100, repostCache, embedder)
	if err != nil {
		return HTML(fmt.Sprint(err))
	}

	err = repostCache.Save(repostCacheFile)
	if err != nil {
		return HTML(fmt.Sprint(err))
	}

	err = embedCache.Save(embedCacheFile)
	if err != nil {
		return HTML(fmt.Sprint(err))
	}

	return c.RenderXml(feed)
}
