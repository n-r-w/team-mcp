package queue

import "github.com/n-r-w/team-mcp/internal/domain"

// deskState stores ordered topic headers for one desk.
type deskState struct {
	topics []domain.TopicHeader
}

// topicState stores ordered message headers for one topic.
type topicState struct {
	messages []domain.MessageHeader
}
