package filesystem

import "os"

const (
	// directoryPermission restricts desk directory access to owner/group only.
	directoryPermission os.FileMode = 0o750
	// filePermission restricts payload/metadata file access to owner/group only.
	filePermission os.FileMode = 0o640
	// markdownExtension keeps persisted message payloads as markdown files.
	markdownExtension = ".md"
	// metaFileName stores desk lifecycle metadata used by cleanup scans.
	metaFileName = "deskmeta.txt"
)
