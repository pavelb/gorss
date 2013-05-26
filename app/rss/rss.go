package rss

import (
	"encoding/xml"
)

type Feed struct {
	XMLName xml.Name `xml:"rss"`
	Version float64  `xml:"version,attr"`
	Channel Channel  `xml:"channel"`
}

type Channel struct {
	XMLName     xml.Name `xml:"channel"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	TTL         int      `xml:"ttl"`
	Items       []*Item
}

type Item struct {
	XMLName     xml.Name `xml:"item"`
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	Comments    string   `xml:"comments"`
	GUID        string   `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
}
