package messagefile

// filePlace locates message file inside one allowed directory.
type filePlace struct {
	// dir is allowed directory that holds file.
	dir string
	// relativePath is path of file inside dir.
	relativePath string
}
