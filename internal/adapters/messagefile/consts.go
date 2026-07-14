package messagefile

import "os"

const (
	// directoryPermission requests owner read/write/traverse and group read/traverse for created parents.
	directoryPermission os.FileMode = 0o750
	// filePermission grants owner read-write and group read access to created message files.
	filePermission os.FileMode = 0o640
	// textExtension is the supported plain-text destination suffix.
	textExtension = ".txt"
	// markdownExtension is the supported Markdown destination suffix.
	markdownExtension = ".md"
)
