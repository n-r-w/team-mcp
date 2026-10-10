package messagefile

import "errors"

var (
	// errFilePathRequired rejects empty destination paths.
	errFilePathRequired = errors.New("file_path is required")
	// errFilePathAbsolute rejects relative destination paths.
	errFilePathAbsolute = errors.New("file_path must be absolute")
	// errFilePathExtension rejects destination formats outside the public contract.
	errFilePathExtension = errors.New("file_path extension must be .txt or .md")
	// errFilePathOutsideAllowedDirs rejects lexical paths that leave each allowed directory.
	errFilePathOutsideAllowedDirs = errors.New("file_path must be inside allowed directory")
	// errSourceExtension rejects message source formats outside the public contract.
	errSourceExtension = errors.New("file_path must have a .txt or .md extension")
	// errSourceNotRegular rejects message sources that are not regular files.
	errSourceNotRegular = errors.New("file_path must reference a regular file")
	// errSourceNotUTF8 rejects message sources with invalid UTF-8 text.
	errSourceNotUTF8 = errors.New("file content must be valid UTF-8")
	// errSourceBlank rejects message sources without visible text.
	errSourceBlank = errors.New("file content is required")
	// errMode rejects write modes outside the closed public contract.
	errMode = errors.New("mode must be create or overwrite")
)
