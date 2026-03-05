package server

// Options defines constructor input and runtime settings for MCP server adapter initialization.
type Options struct {
	Version             string
	MaxTitleLength      int
	CoordinationUseCase ICoordination
	ToolDescriptions    ToolDescriptions
	SystemPrompt        string
}

// ToolDescriptions defines optional MCP tool description overrides.
type ToolDescriptions struct {
	DeskCreate    string
	DeskRemove    string
	TopicCreate   string
	TopicList     string
	MessageCreate string
	MessageList   string
	MessageGet    string
}
