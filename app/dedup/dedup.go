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
	fmt.Println("fetching: " + url)
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
	const hashCachePath = "dedup-hash-cache"
	hashCache, err := cache.LoadLRUS(100*1024, hashCachePath)
	if err != nil {
		return
	}
	hashes := hashItems(feed.Channel.Items, hashCache)
	err = hashCache.Save(hashCachePath)
	if err != nil {
		return
	}

	allItemsCachePath := getAllItemsCachePath(guid)
	recentItemsCachePath := getRecentItemsCachePath(guid, feed)

	allItemsCache, err := cache.LoadLRUS(100*1024, allItemsCachePath)
	if err != nil {
		return
	}
	recentItemsCache, err := cache.LoadLRUS(1024, recentItemsCachePath)
	if err != nil {
		return
	}
	newItemsCache := make(map[string]string)

	var rv []*rss.Item
	for i, item := range feed.Channel.Items {
		hash := hashes[i]

		fmt.Print(item.Link + ": ")

		if _, ok := recentItemsCache.Get(hash); ok {
			fmt.Println("found in recent items, (recently) new")
		} else if _, ok := allItemsCache.Get(hash); ok {
			fmt.Println("not in recent but found in all, duplicate")
			continue
		} else {
			fmt.Println("never seen before, new")
			recentItemsCache.Set(hash, "")
			allItemsCache.Set(hash, "")
		}

		if _, ok := newItemsCache[hash]; ok {
			fmt.Println("found in current feed, duplicate")
			continue
		}
		newItemsCache[hash] = ""
		rv = append(rv, item)
	}
	feed.Channel.Items = rv

	err = allItemsCache.Save(allItemsCachePath)
	if err != nil {
		return
	}
	return recentItemsCache.Save(recentItemsCachePath)
}
