package runstate

import "time"

// deskRecord stores desk metadata and topic lookup indexes.
type deskRecord struct {
	id           string
	createdAt    time.Time
	topicIDs     []string
	topicByTitle map[string]string
}

// topicRecord stores topic metadata and message lookup indexes.
type topicRecord struct {
	deskID                   string
	messageIDs               []string
	messageByNormalizedTitle map[string]string
}

// messageRecord stores message metadata needed for duplicate checks and lookups.
type messageRecord struct {
	id              string
	deskID          string
	topicID         string
	title           string
	normalizedTitle string
}
