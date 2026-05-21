package storage

import "errors"

// ErrNotFound marks storage lookups that did not find a requested resource.
var ErrNotFound = errors.New("not found")
