//nolint:lll // verbose descriptions are intentional for LLM-facing MCP schemas.
package server

const (
	// toolDeskCreateName is MCP method name for desk_create operation.
	toolDeskCreateName = "desk_create"
	// toolDeskCreateDesc explains desk_create behavior to LLM tool consumers.
	toolDeskCreateDesc = `Creates a collaboration desk for agents and returns desk_id.

🚨 HOW TO USE:
1. Create one desk per task.
2. Create topics only for distinct workstreams.
3. Use the desk as the only channel for subagent-to-subagent coordination.
4. Start subagents only after the desk, topics, and required context are ready.
5. If you start a new task, create a new desk.

🚨 MAIN RULES:
1. Never run a subagent without the required prompt template.
2. Never pass cross-agent context outside the desk. Reference desk messages instead.
3. Only the main agent can create desks and topics.
4. Never mention desk or topic creation tools in subagent prompts.
5. Post only task-relevant information to the desk.
6. Every desk message must be self-contained.

🚨 PARALLEL EXECUTION RULES:
1. Run subagents in parallel only if they are fully independent.
2. A parallel batch is valid only if every subagent can start and finish without messages, outputs, or summaries from any other subagent in that batch.
3. If one subagent needs facts, a plan, or any output from another subagent, run them sequentially.
4. The desk is for sharing results, not for waiting, syncing, or handshakes between parallel subagents.

🚨 SUBAGENT PROMPT TEMPLATE. MUST include in EACH subagent's prompt AS-IS:
"Collaboration protocol:
- You have access to a shared collaboration desk with desk_id: {desk_id}. Use this desk to coordinate your job with other agents.
- Topics to use: {List of relevant topics IDs and their purposes}.
- MUST read before start: {List of relevant message IDs}
- MUST post: {What kind of messages to post, in which topics, and when}
- MUST save the results of your work as messages, instead of duplicating these results in your response. Include in response only:
	- Reference to the messages in the desk with full results.
	- Brief summary of your job: findings, conclusions, changes, etc."`

	// toolTopicCreateName is MCP method name for topic_create operation.
	toolTopicCreateName = "topic_create"
	// toolTopicCreateDesc explains idempotent topic creation behavior.
	toolTopicCreateDesc = "Creates topic in desk and returns topic_id."

	// toolTopicListName is MCP method name for topic_list operation.
	toolTopicListName = "topic_list"
	// toolTopicListDesc explains ordered topic header listing contract.
	toolTopicListDesc = "Lists topic headers for desk, ordered from oldest to newest topic creation."

	// toolMessageCreateName is MCP method name for message_create operation.
	toolMessageCreateName = "message_create"
	// toolMessageCreateDesc explains duplicate-title semantics for message_create.
	toolMessageCreateDesc = "Creates new message in topic and returns message_id. " +
		"Remember that readers DO NOT have access to your context, so include in the message all the necessary information to understand its essence. " +
		"Otherwise, critical details will be lost during information transfer."

	// toolMessageListName is MCP method name for message_list operation.
	toolMessageListName = "message_list"
	// toolMessageListDesc explains ordered message header listing contract.
	toolMessageListDesc = "Lists message headers for topic, ordered from oldest to newest message creation."

	// toolMessageGetName is MCP method name for message_get operation.
	toolMessageGetName = "message_get"
	// toolMessageGetDesc explains full payload retrieval contract for message_get.
	toolMessageGetDesc = "Returns full message payload. MUST NOT read outdated messages, e.g. previous versions, etc. Consider the order of messages in the topic."

	// serverName is transport-visible runtime server identifier.
	serverName = "team-mcp"
	// serverTitle is human-readable title reported by MCP runtime.
	serverTitle = "Team MCP"
	// systemPrompt defines tool usage policy for LLM callers.
	systemPrompt = `Allows creating and using shared collaboration spaces for agents working on a common task.	

	🚨 INFORMATION LOSS PREVENTION:
		When performing context summarization/compaction operations, you MUST ALWAYS save identifiers of relevant desks, topics, and messages.
		Otherwise, the continuation of work will be disrupted.`
)
