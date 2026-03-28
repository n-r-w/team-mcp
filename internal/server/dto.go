package server

// deskCreateInput is MCP input DTO for desk_create tool.
type deskCreateInput struct {
	// No input required
}

// deskCreateOutput is MCP output DTO for desk_create tool.
type deskCreateOutput struct {
	DeskID string `json:"desk_id"`
}

// topicCreateInput is MCP input DTO for topic_create tool.
type topicCreateInput struct {
	DeskID string `json:"desk_id" jsonschema:"desk identifier. required"`
	Title  string `json:"title" jsonschema:"topic title. required"`
}

// topicCreateOutput is MCP output DTO for topic_create tool.
type topicCreateOutput struct {
	Status  string `json:"status,omitempty"`
	TopicID string `json:"topic_id,omitempty"`
}

// topicListInput is MCP input DTO for topic_list tool.
type topicListInput struct {
	DeskID string `json:"desk_id" jsonschema:"desk identifier. required"`
}

// topicListOutput is MCP output DTO for topic_list tool.
type topicListOutput struct {
	Status string           `json:"status,omitempty"`
	Topics []topicHeaderDTO `json:"topics,omitempty"`
}

// topicHeaderDTO is one topic list item.
type topicHeaderDTO struct {
	TopicID string `json:"topic_id"`
	Title   string `json:"title"`
}

// messageCreateInput is MCP input DTO for message_create tool.
type messageCreateInput struct {
	TopicID string `json:"topic_id" jsonschema:"topic identifier. required"`
	Title   string `json:"title" jsonschema:"message title. required"`
	Content string `json:"content" jsonschema:"message markdown content. required"`
}

// messageCreateOutput is MCP output DTO for message_create tool.
type messageCreateOutput struct {
	Status            string `json:"status,omitempty"`
	MessageID         string `json:"message_id,omitempty"`
	ExistingMessageID string `json:"existing_message_id,omitempty"`
	StatusMessage     string `json:"status_message,omitempty"`
}

// messageListInput is MCP input DTO for message_list tool.
type messageListInput struct {
	TopicID string `json:"topic_id" jsonschema:"topic identifier. required"`
}

// messageListOutput is MCP output DTO for message_list tool.
type messageListOutput struct {
	Status   string             `json:"status,omitempty"`
	Messages []messageHeaderDTO `json:"messages,omitempty"`
}

// messageHeaderDTO is one message list item.
type messageHeaderDTO struct {
	MessageID string `json:"message_id"`
	Title     string `json:"title"`
}

// messageGetInput is MCP input DTO for message_get tool.
type messageGetInput struct {
	MessageID string `json:"message_id" jsonschema:"message identifier. required"`
}

// messageGetOutput is MCP output DTO for message_get tool.
type messageGetOutput struct {
	Status  string `json:"status,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
}
