package domain

import "errors"

// ErrNotFound is the shared not-found sentinel of the storage ports: one
// sentinel across the scenarios and repositories, because a repository
// method serves several scenario ports and must speak the not-found
// every consumer checks.
var ErrNotFound = errors.New("domain: not found")
