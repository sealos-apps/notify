package storage

import "errors"

// ErrNotFound marks storage lookups that did not find a requested resource.
var ErrNotFound = errors.New("not found")

// ErrLeaseNotHeld marks task updates attempted by a dispatcher that no longer owns the task lease.
var ErrLeaseNotHeld = errors.New("lease not held")
