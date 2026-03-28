package usecase

const (
	// eventMessageCreate identifies message_create operation in structured logs.
	eventMessageCreate = "message_create"
	// eventDeskCreate identifies desk_create operation in structured logs.
	eventDeskCreate = "desk_create"
	// eventStartupGC identifies startup expired-desk cleanup event.
	eventStartupGC = "startup_gc"
	// eventRuntimeGC identifies runtime TTL cleanup event.
	eventRuntimeGC = "runtime_gc"
	// logFieldEvent is a structured log key for operation event name.
	logFieldEvent = "event"
	// logFieldResult is a structured log key for cleanup result.
	logFieldResult = "result"
	// logFieldStatus is a structured log key for operation status.
	logFieldStatus = "status"
	// logFieldDeskID is a structured log key for desk identifier.
	logFieldDeskID = "desk_id"
	// logFieldTopicID is a structured log key for topic identifier.
	logFieldTopicID = "topic_id"
	// logFieldMessageID is a structured log key for message identifier.
	logFieldMessageID = "message_id"
	// logFieldExistingMessageID is a structured log key for existing duplicate message identifier.
	logFieldExistingMessageID = "existing_message_id"
	// logFieldError is a structured log key for operation error payload.
	logFieldError = "error"
	// logResultOK indicates cleanup completed without errors.
	logResultOK = "ok"
	// logResultError indicates cleanup completed with errors.
	logResultError = "error"
)
