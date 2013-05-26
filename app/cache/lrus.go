package cache

import (
	"encoding/gob"
	"os"
)

type LRUS struct {
	lru *LRUCache
}

type cacheValue struct {
	Value string
}

type savableItem struct {
	Key   string
	Value string
}

func (cv *cacheValue) Size() int {
	return len(cv.Value)
}

func NewLRUS(capacity uint64) *LRUS {
	return &LRUS{NewLRUCache(capacity)}
}

func (lrus *LRUS) Set(key string, value string) {
	lrus.lru.Set(key, &cacheValue{value})
}

func (lrus *LRUS) Get(key string) (value string, ok bool) {
	if cached, ok := lrus.lru.Get(key); ok {
		value = cached.(*cacheValue).Value
	}
	return
}

func LoadLRUS(capacity uint64, file string) (lrus *LRUS, err error) {
	lrus = NewLRUS(capacity)

	fileHandle, err := os.OpenFile(file, os.O_RDONLY, 0600)
	if err != nil {
		return lrus, nil
	}
	defer fileHandle.Close()

	var items []savableItem
	err = gob.NewDecoder(fileHandle).Decode(&items)
	if err != nil {
		return
	}
	for _, item := range items {
		lrus.Set(item.Key, item.Value)
	}
	return
}

func (lrus *LRUS) Save(file string) (err error) {
	fileHandle, err := os.Create(file)
	if err != nil {
		return
	}
	defer fileHandle.Close()

	var items []savableItem
	for _, item := range lrus.lru.Items() {
		items = append(items, savableItem{
			Key:   item.Key,
			Value: item.Value.(*cacheValue).Value,
		})
	}
	return gob.NewEncoder(fileHandle).Encode(items)
}
