package filesystem

import "errors"

var (
	// errBoardVersionConflict reports that another writer committed the next metadata version first.
	errBoardVersionConflict = errors.New("board metadata version conflict")
)
