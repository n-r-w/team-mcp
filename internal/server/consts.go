//nolint:lll // verbose descriptions are intentional for LLM-facing MCP schemas.
package server

const (
	// toolDeskCreateName is MCP method name for desk_create operation.
	toolDeskCreateName = "desk_create"
	// toolDeskCreateDesc explains desk_create behavior to LLM tool consumers.
	toolDeskCreateDesc = `Creates a collaboration desk for agents and returns desk_id.

HOW TO USE:
1. Design the most effective set of subagents to solve the task.
2. Call desk_create to create a new desk and obtain its desk_id.
3. Call topic_create to create topics within the desk for different discussion threads or coordination needs.
4. Coordinate subagent's interactions through the shared desk.
5. When the desk is no longer needed, call desk_remove to clean up resources.

CRITICAL RULES:
1. Collaboration desk is the MAIN communication channel for subagents.
2. MUST NEVER execute subagent without "SUBAGENT PROMPT TEMPLATE"!
3. MUST NEVER bypass the desk in communication! E.g.:
	- Planner subagent creates a plan and posts it in the desk
	- GOOD: Main agent execute coding subagent and add to its prompt reference to the plan message in the desk.
	   BAD: Main agent execute coding subagent and add to its prompt implementation instructions directly, without referencing the plan message in the desk.
	   WHY BAD: Critical details will be lost during information transfer.
4. Subagents DO NOT have access to tools desk_create and desk_remove. Only the main agent can create or remove desks.
5. NEVER mention desk_create, desk_remove and topic_create in subagent prompts, they DO NOT KNOW about these tools.
6. CRITICAL: NEVER run subagents in parallel, that depend on each other. For example:
    - A developer creates a feature, and a tester needs to test it.
	- If you run these subagents in parallel, the tester will start testing before the developer creates the feature, leading to errors.
	- REMEMBER: Subagents CANNOT WAIT for each other's results!

SUBAGENT PROMPT TEMPLATE FOR CONSISTENT COMMUNICATION. MUST include in EACH subagent's prompt AS-IS:
"Collaboration protocol:
- You have access to a shared collaboration desk with desk_id: {desk_id}. Use this desk to coordinate your job with other agents.
- Your teammates: {List of subagents and their roles}.
- Topics to use: {List of relevant topics IDs and their purposes}.
- MUST read before start: {List of relevant message IDs}
- MUST post: {What kind of messages to post, in which topics, and when.}
- MUST save the results of your work as messages, instead of duplicating these results in your response. Include in response only:
	- Reference to the messages in the desk with full results.
	- Brief summary of your job: findings, conclusions, changes, etc."`

	// toolDeskRemoveName is MCP method name for desk_remove operation.
	toolDeskRemoveName = "desk_remove"
	// toolDeskRemoveDesc explains synchronous cascade semantics for desk_remove.
	toolDeskRemoveDesc = "Removes desk and all linked topics/messages from memory and disk. MUST be called to clean up resources when desk is no longer needed."

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
	systemPrompt = `Allows creating and using shared collaboration spaces for agents working on a common task.`
)
