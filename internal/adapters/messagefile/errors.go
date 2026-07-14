package messagefile

import "errors"

var (
	// errFilePathRequired rejects empty destination paths.
	errFilePathRequired = errors.New("file_path is required")
	// errFilePathAbsolute rejects relative destination paths.
	errFilePathAbsolute = errors.New("file_path must be absolute")
	// errFilePathExtension rejects destination formats outside the public contract.
	errFilePathExtension = errors.New("file_path extension must be .txt or .md")
	// errFilePathOutsideHome rejects lexical paths that leave the configured home directory.
	errFilePathOutsideHome = errors.New("file_path must be inside user home directory")
	// errMode rejects write modes outside the closed public contract.
	errMode = errors.New("mode must be create or overwrite")
)
