//nolint:lll // verbose descriptions are intentional for LLM-facing MCP schemas.
package server

const (
	// keepIDsRule tells orchestrators, authors, and readers of desk to keep desk references through context compaction.
	keepIDsRule = "When you summarize or compact context, MUST keep IDs of relevant desks, topics, and messages: " +
		"otherwise you cannot continue work with desk."

	// toolDeskCreateName is MCP method name for desk_create operation.
	toolDeskCreateName = "desk_create"
	// toolDeskCreateDesc explains desk_create behavior to LLM tool consumers.
	toolDeskCreateDesc = "Creates a collaboration desk for one task and returns desk_id. " +
		"Agents share results of task through messages in topics of desk. " +
		"Users DO NOT SEE desk contents, even when IDs are provided. " +
		keepIDsRule

	// toolTopicCreateName is MCP method name for topic_create operation.
	toolTopicCreateName = "topic_create"
	// toolTopicCreateDesc explains idempotent topic creation behavior.
	toolTopicCreateDesc = "Creates topic in desk for one workstream of task and returns topic_id."

	// toolTopicListName is MCP method name for topic_list operation.
	toolTopicListName = "topic_list"
	// toolTopicListDesc explains ordered topic header listing contract.
	toolTopicListDesc = "Lists topic headers for desk, ordered from oldest to newest topic creation."

	// toolMessageCreateName is MCP method name for message_create operation.
	toolMessageCreateName = "message_create"
	// toolMessageCreateDesc explains content-source and duplicate-title semantics for message_create.
	toolMessageCreateDesc = "Creates new message in topic and returns message_id. " +
		"Provide exactly one content source: content with message text, or file_path with an absolute path to a UTF-8 .txt or .md file inside user home directory or temporary directory (system temporary directory; on Unix also /tmp). " +
		"Post only information that task needs. " +
		"Remember that readers DO NOT have access to your context, so include in message all necessary information to understand its essence. " +
		"Otherwise, critical details will be lost during information transfer. " +
		"For complex tasks, prefer preparing message in a file and passing it via file_path, as this allows you to refine and plan content in advance. " +
		keepIDsRule

	// toolMessageListName is MCP method name for message_list operation.
	toolMessageListName = "message_list"
	// toolMessageListDesc explains ordered message header listing contract.
	toolMessageListDesc = "Lists message headers for topic, ordered from oldest to newest message creation."

	// toolMessageGetName is MCP method name for message_get operation.
	toolMessageGetName = "message_get"
	// toolMessageGetDesc explains full payload retrieval contract for message_get.
	toolMessageGetDesc = "Returns full message payload. MUST NOT read outdated messages, e.g. previous versions, etc. Consider order of messages in topic. " +
		keepIDsRule

	// toolSaveMessageToFileName is MCP method name for save_message_to_file operation.
	toolSaveMessageToFileName = "save_message_to_file"
	// toolSaveMessageToFileDesc explains constrained message export contract.
	toolSaveMessageToFileDesc = "Saves a message payload to an absolute .txt or .md file inside user home directory or temporary directory " +
		"(system temporary directory; on Unix also /tmp). Mode must be create or overwrite."

	// serverName is transport-visible runtime server identifier.
	serverName = "team-mcp"
	// serverTitle is human-readable title reported by MCP runtime.
	serverTitle = "Team MCP"
)
