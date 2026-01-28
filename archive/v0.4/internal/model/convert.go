// Package model provides data types and conversion functions.
//
// WARNING: Converting model.Item to feeds.Item and back LOSES the Embedding field.
// feeds.Item does not support embeddings, so round-trip conversions will have nil Embedding.
// Use these conversions only at system boundaries, not for data persistence.
package model

import "github.com/abelbrown/observer/internal/feeds"

// FromFeedsItem converts feeds.Item to model.Item.
// Note: Embedding is set to nil since feeds.Item doesn't support embeddings.
// This is intentional - embeddings are populated separately by the embedding service.
func FromFeedsItem(f feeds.Item) Item {
	return Item{
		ID:         f.ID,
		Source:     SourceType(f.Source), // Convert feeds.SourceType to model.SourceType
		SourceName: f.SourceName,
		SourceURL:  f.SourceURL,
		Title:      f.Title,
		Summary:    f.Summary,
		Content:    f.Content,
		URL:        f.URL,
		Author:     f.Author,
		Published:  f.Published,
		Fetched:    f.Fetched,
		Read:       f.Read,
		Saved:      f.Saved,
		Embedding:  nil, // feeds.Item doesn't have embeddings
	}
}

// ToFeedsItem converts model.Item to feeds.Item.
// WARNING: Embedding data is lost in this conversion.
// Do not use this if you need to preserve embeddings.
func (m Item) ToFeedsItem() feeds.Item {
	return feeds.Item{
		ID:         m.ID,
		Source:     feeds.SourceType(m.Source), // Convert model.SourceType to feeds.SourceType
		SourceName: m.SourceName,
		SourceURL:  m.SourceURL,
		Title:      m.Title,
		Summary:    m.Summary,
		Content:    m.Content,
		URL:        m.URL,
		Author:     m.Author,
		Published:  m.Published,
		Fetched:    m.Fetched,
		Read:       m.Read,
		Saved:      m.Saved,
	}
}

// FromFeedsItems converts a slice of feeds.Item to a slice of model.Item.
func FromFeedsItems(items []feeds.Item) []Item {
	result := make([]Item, len(items))
	for i, item := range items {
		result[i] = FromFeedsItem(item)
	}
	return result
}

// ToFeedsItems converts a slice of model.Item to a slice of feeds.Item.
func ToFeedsItems(items []Item) []feeds.Item {
	result := make([]feeds.Item, len(items))
	for i, item := range items {
		result[i] = item.ToFeedsItem()
	}
	return result
}
