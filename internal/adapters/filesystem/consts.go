package filesystem

import "os"

const (
	// directoryPermission restricts desk directory access to owner/group only.
	directoryPermission os.FileMode = 0o750
	// filePermission restricts payload/metadata file access to owner/group only.
	filePermission os.FileMode = 0o640
	// markdownExtension keeps persisted message payloads as markdown files.
	markdownExtension = ".md"
	// boardDeskStateFileName stores the latest mirrored desk metadata snapshot.
	boardDeskStateFileName = "desk.json"
	// boardJSONExtension keeps authoritative metadata files in JSON format.
	boardJSONExtension = ".json"
	// boardTopicsDirName keeps per-topic metadata under each desk.
	boardTopicsDirName = "topics"
	// boardTopicLookupDirName stores direct topic-to-desk lookup files.
	boardTopicLookupDirName = "_topics"
	// boardMessageLookupDirName stores direct message-to-metadata lookup files.
	boardMessageLookupDirName = "_messages"
	// boardVersionsDirName keeps authoritative version snapshots for optimistic retry.
	boardVersionsDirName = ".versions"
	// boardVersionsSuffix distinguishes per-topic version directories from topic metadata files.
	boardVersionsSuffix = ".versions"
	// boardMaxVersionRetries bounds optimistic retry loops during concurrent mutations.
	boardMaxVersionRetries = 32
)
