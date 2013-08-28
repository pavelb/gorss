package dedup

import (
	"crypto/md5"
	"fmt"
	"github.com/pavelb/gorss/app/cache"
	"github.com/pavelb/gorss/app/rss"
	"io"
	"net/http"
	"regexp"
	"sync"
)

func getHashDirect(url string) (hash string, err error) {
	fmt.Println("hashing " + url)
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	h := md5.New()

	_, err = io.Copy(h, resp.Body)
	if err != nil {
		return
	}

	hash = fmt.Sprintf("%x", h.Sum(nil))
	fmt.Println("hashed " + url)
	return
}

func getHash(url string, cache *cache.LRUS) (hash string, err error) {
	hash, ok := cache.Get(url)
	if !ok {
		hash, err = getHashDirect(url)
		if err != nil {
			return
		}
	}
	cache.Set(url, hash)
	return
}

func hashItems(items []*rss.Item, cache *cache.LRUS) []string {
	hashes := make([]string, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(i int, item *rss.Item) {
			defer wg.Done()
			f, err := getHash(item.Link, cache)
			if err != nil {
				return
			}
			hashes[i] = f
		}(i, item)
	}
	wg.Wait()
	return hashes
}

func getAllItemsCachePath(guid string) string {
	return "dedup-" + guid + "-all-items"
}

func getRecentItemsCachePath(guid string, feed *rss.Feed) string {
	re := regexp.MustCompile("[^a-zA-Z]+")
	feedGuid := re.ReplaceAllString(feed.Channel.Link, "-")
	return "dedup-" + guid + "-" + feedGuid
}

func Dedup(feed *rss.Feed, guid string) (err error) {
	// Hash item links, cache results.
	const urlToHashCachePath = "dedup-hash-cache"
	urlToHashCache, err := cache.LoadLRUS(100000, urlToHashCachePath)
	if err != nil {
		return
	}
	hashes := hashItems(feed.Channel.Items, urlToHashCache)
	err = urlToHashCache.Save(urlToHashCachePath)
	if err != nil {
		return
	}

	// Init served links hash caches.
	allItemsCachePath := getAllItemsCachePath(guid)
	allItemsCache, err := cache.LoadLRUS(100000, allItemsCachePath)
	if err != nil {
		return
	}
	recentItemsCachePath := getRecentItemsCachePath(guid, feed)
	recentItemsCache, err := cache.LoadLRUS(100000, recentItemsCachePath)
	if err != nil {
		return
	}
	newItemsCache := cache.NewLRUS(100000)

	// Prune duplicate links.
	var rv []*rss.Item
	for i, item := range feed.Channel.Items {
		hash := hashes[i]

		fmt.Print(item.Link + ": ")

		if _, ok := newItemsCache.Get(hash); ok {
			fmt.Println("found in new feed, duplicate")
			continue
		} else if _, ok := recentItemsCache.Get(hash); ok {
			fmt.Println("found in last feed, new")
		} else if _, ok := allItemsCache.Get(hash); ok {
			fmt.Println("found in global feed, duplicate")
			continue
		} else {
			fmt.Println("not found in any feed, new")
			recentItemsCache.Set(hash, "")
			allItemsCache.Set(hash, "")
		}

		newItemsCache.Set(hash, "")
		rv = append(rv, item)
	}
	feed.Channel.Items = rv

	// Save served links hash caches.
	err = allItemsCache.Save(allItemsCachePath)
	if err != nil {
		return
	}
	return newItemsCache.Save(recentItemsCachePath)
}
