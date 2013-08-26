package dedup

import (
	"crypto/md5"
	"fmt"
	// "github.com/pavelb/gorss/app/cache"
	"encoding/gob"
	"github.com/pavelb/gorss/app/rss"
	"io"
	"net/http"
	"os"
	"regexp"
	"sync"
)

func fingerprint_(url string) (hash string, err error) {
	return "123", nil

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

func save(items []*rss.Item, path string) (err error) {
	fileHandle, err := os.Create(path)
	if err != nil {
		return
	}
	defer fileHandle.Close()
	return gob.NewEncoder(fileHandle).Encode(items)
}

func load(path string) (items []*rss.Item, err error) {
	return

	_, err = os.Stat(path)
	if err != nil {
		fmt.Println("Stored items not found, returning empty slice.")
		return items, nil // Return empty slice.
	}

	fileHandle, err := os.OpenFile(path, os.O_RDONLY, 0600)
	if err != nil {
		return
	}
	defer fileHandle.Close()

	err = gob.NewDecoder(fileHandle).Decode(&items)
	return
}

func fingerprintItems(items []*rss.Item) []string {
	fingerprints := make([]string, len(items))

	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(i int, item *rss.Item) {
			defer wg.Done()
			f, err := fingerprint_(item.Link)
			if err != nil {
				return
			}
			fingerprints[i] = f
		}(i, item)
	}
	wg.Wait()

	return fingerprints
}

func merge(master_items *[]*rss.Item, new_items []*rss.Item) {
	master_fingerprints := make(map[string]bool)
	for _, f := range fingerprintItems(*master_items) {
		if f != "" {
			master_fingerprints[f] = true
		}
	}

	for i, f := range fingerprintItems(new_items) {
		if _, found := master_fingerprints[f]; !found {
			*master_items = append(*master_items, new_items[i])
		}
	}
}

func truncate(items *[]*rss.Item, length int) {
	index := len(*items) - 1 - length
	if index >= 0 {
		*items = (*items)[index:]
	}
}

func Dedup(feed *rss.Feed, guid string) (err error) {
	re := regexp.MustCompile("[^a-zA-Z]+")
	feedGuid := re.ReplaceAllString(feed.Channel.Link, "-")
	path := "dedup-last-feed-" + guid + "-" + feedGuid

	items, err := load(path)
	if err != nil {
		return
	}

	merge(&items, feed.Channel.Items)

	truncate(&items, 100000)
	err = save(items, path)
	if err != nil {
		return
	}

	truncate(&items, 100)
	feed.Channel.Items = items

	// const dedupCacheFile = "embedCache"
	// embedCache, err := cache.LoadLRUS(100*1024, dedupCacheFile)
	// if err != nil {
	// 	return HTML(fmt.Sprint(err))
	// }

	return
}
