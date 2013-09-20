package controllers

import (
	"encoding/json"
	"fmt"
	"github.com/pavelb/gorss/app/cache"
	"github.com/pavelb/gorss/app/dedup"
	"github.com/pavelb/gorss/app/embed"
	"github.com/pavelb/gorss/app/rss"
	"github.com/robfig/revel"
	"io/ioutil"
	"net/http"
	"strings"
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

func getURL(url string) (bytes []byte, err error) {
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func newRSSItem(jsonItem *redditJSONItem, embedder *embed.Embedder) *rss.Item {
	if jsonItem.Over_18 {
		return nil
	}

	rv := new(rss.Item)
	rv.Title = jsonItem.Title
	rv.Comments = "http://www.reddit.com" + jsonItem.Permalink

	embedInfo, err := embedder.Embed(jsonItem.URL)
	if err == nil {
		rv.Link = embedInfo.URL
		rv.Description = embedInfo.Html
	} else {
		rv.Link = jsonItem.URL
		rv.Description = err.Error()
	}
	rv.Description += "<br/><br/><a href='" + rv.Comments + "'>Comments</a>"
	if rv.Link != jsonItem.URL {
		rv.Description += "&nbsp;&nbsp;&nbsp;&nbsp;"
		rv.Description += "<a href='" + jsonItem.URL + "'>Original</a>"
	}

	rv.PubDate = time.Unix(int64(jsonItem.Created_utc), 0).Format(time.RFC822)
	return rv
}

func getRedditJSONFeed(subreddit string) (jsonFeed *redditJSONFeed, err error) {
	const urlTpl = "http://www.reddit.com/r/%s.json?limit=100"
	bytes, err := getURL(fmt.Sprintf(urlTpl, subreddit))
	if err != nil {
		return
	}
	jsonFeed = new(redditJSONFeed)
	err = json.Unmarshal(bytes, jsonFeed)
	return
}

func getRedditXMLFeed(subreddit string, embedder *embed.Embedder) (
	feed *rss.Feed, err error) {

	jsonFeed, err := getRedditJSONFeed(subreddit)
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
				embedder,
			)
		}(i)
	}
	wg.Wait()

	// Remove nil items
	var items []*rss.Item
	for _, item := range feed.Channel.Items {
		if item != nil {
			items = append(items, item)
		}
	}
	feed.Channel.Items = items

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
	parts := strings.Split(r, ":")
	r = parts[0]
	guid := "test"
	if len(parts) > 1 {
		guid = parts[1]
	}

	const embedCacheFile = "embedCache"

	embedCache, err := cache.LoadLRUS(100*1024, embedCacheFile)
	if err != nil {
		return HTML(fmt.Sprint(err))
	}
	embedder := embed.NewEmbedder(embedCache, map[string]string{
		"EmbedlyAPIKey": "8b02b918d50e4e33b9152d62985d6241",
		"maxWidth":      "768",
	})

	feed, err := getRedditXMLFeed(r, embedder)
	if err != nil {
		return HTML("Cant get xml feed: " + fmt.Sprint(err))
	}

	err = dedup.Dedup(feed, guid)
	if err != nil {
		return HTML("Cant dedup: " + fmt.Sprint(err))
	}

	err = embedCache.Save(embedCacheFile)
	if err != nil {
		return HTML("Cant save embed cache: " + fmt.Sprint(err))
	}

	return c.RenderXml(feed)
}
