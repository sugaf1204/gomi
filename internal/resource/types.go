package resource

import "errors"

var ErrNotFound = errors.New("not found")

// ErrAlreadyExists reports that a record with the same name is already stored.
// Stores return it from insert-only writes so callers can reject a duplicate
// instead of silently overwriting the existing record.
var ErrAlreadyExists = errors.New("already exists")
