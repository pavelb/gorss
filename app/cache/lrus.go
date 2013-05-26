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

type SavableItem struct {
	Key   string
	Value string
}

func (lrus *LRUS) Size() uint64 {
	return lrus.lru.size
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
	cached, ok := lrus.lru.Get(key)
	if ok {
		value = cached.(*cacheValue).Value
	}
	return
}

func LoadLRUS(capacity uint64, file string) (lrus *LRUS, err error) {
	lrus = NewLRUS(capacity)

	fileHandle, err := os.OpenFile(file, os.O_RDONLY, 0600)
	if err != nil {
		err = nil
		return
	}
	defer fileHandle.Close()

	decoder := gob.NewDecoder(fileHandle)
	var items []SavableItem
	err = decoder.Decode(&items)
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

	var si []SavableItem
	for _, item := range lrus.lru.Items() {
		si = append(si, SavableItem{
			Key:   item.Key,
			Value: item.Value.(*cacheValue).Value,
		})
	}
	gob.NewEncoder(fileHandle).Encode(si)
	return
}
