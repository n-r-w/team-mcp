package domain

// BusinessStatus describes deterministic non-error business outcomes.
type BusinessStatus string

// MessageFileMode selects destination file creation behavior.
type MessageFileMode string

const (
	// BusinessStatusOK indicates successful completion for operations that return status field.
	BusinessStatusOK BusinessStatus = "ok"
	// BusinessStatusNotFound indicates referenced desk/topic/message does not exist.
	BusinessStatusNotFound BusinessStatus = "not_found"
	// BusinessStatusDuplicateTitle indicates duplicate normalized message title within the same topic.
	BusinessStatusDuplicateTitle BusinessStatus = "duplicate_title"

	// MessageFileModeCreate creates a destination only when it does not exist.
	MessageFileModeCreate MessageFileMode = "create"
	// MessageFileModeOverwrite creates or truncates the destination.
	MessageFileModeOverwrite MessageFileMode = "overwrite"
)
