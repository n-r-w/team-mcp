package domain

import "time"

// DeskCreateResult contains output for desk_create operation.
type DeskCreateResult struct {
	DeskID string
}

// DeskRemoveRequest contains input for desk_remove operation.
type DeskRemoveRequest struct {
	DeskID string
}

// DeskRemoveResult contains output for desk_remove operation.
type DeskRemoveResult struct {
	Status BusinessStatus
}

// TopicCreateRequest contains input for topic_create operation.
type TopicCreateRequest struct {
	DeskID string
	Title  string
}

// TopicCreateResult contains output for topic_create operation.
type TopicCreateResult struct {
	Status  BusinessStatus
	TopicID string
}

// TopicListRequest contains input for topic_list operation.
type TopicListRequest struct {
	DeskID string
}

// TopicListResult contains output for topic_list operation.
type TopicListResult struct {
	Status BusinessStatus
	Topics []TopicHeader
}

// TopicHeader is a list item returned by topic_list.
type TopicHeader struct {
	TopicID string
	Title   string
}

// MessageCreateRequest contains input for message_create operation.
type MessageCreateRequest struct {
	TopicID string
	Title   string
	Content string
}

// MessageCreateResult contains output for message_create operation.
type MessageCreateResult struct {
	Status            BusinessStatus
	MessageID         string
	ExistingMessageID string
	StatusMessage     string
}

// MessageListRequest contains input for message_list operation.
type MessageListRequest struct {
	TopicID string
}

// MessageListResult contains output for message_list operation.
type MessageListResult struct {
	Status   BusinessStatus
	Messages []MessageHeader
}

// MessageHeader is a list item returned by message_list.
type MessageHeader struct {
	MessageID string
	Title     string
}

// MessageGetRequest contains input for message_get operation.
type MessageGetRequest struct {
	MessageID string
}

// MessageGetResult contains output for message_get operation.
type MessageGetResult struct {
	Status  BusinessStatus
	Title   string
	Content string
}

// DeskSnapshot contains in-memory references for one desk used during cascade removal.
type DeskSnapshot struct {
	DeskID     string
	CreatedAt  time.Time
	TopicIDs   []string
	MessageIDs []string
}

// MessageMeta contains in-memory message metadata used for reads and cleanup.
type MessageMeta struct {
	MessageID string
	TopicID   string
	DeskID    string
	Title     string
}
