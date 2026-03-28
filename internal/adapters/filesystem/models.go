package filesystem

import "github.com/n-r-w/team-mcp/internal/domain"

// boardDeskState keeps authoritative desk metadata that drives topic ordering and TTL scanning.
type boardDeskState struct {
	Version      int64                `json:"version"`
	CreatedAt    int64                `json:"created_at"`
	Topics       []domain.TopicHeader `json:"topics"`
	TopicByTitle map[string]string    `json:"topic_by_title"`
}

// boardTopicState keeps authoritative topic metadata that drives message ordering and dedupe.
type boardTopicState struct {
	Version                  int64                  `json:"version"`
	Messages                 []domain.MessageHeader `json:"messages"`
	MessageByNormalizedTitle map[string]string      `json:"message_by_normalized_title"`
}

// boardTopicLookup resolves topic ownership back to its desk without scanning all desks.
type boardTopicLookup struct {
	DeskID string `json:"desk_id"`
}

// boardMessageLookup resolves message ownership back to desk and topic through direct lookup files.
type boardMessageLookup struct {
	DeskID  string `json:"desk_id"`
	TopicID string `json:"topic_id"`
}
