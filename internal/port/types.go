package port

import "errors"

// WorkloadID identifies a prepared workload.
type WorkloadID string

// TenantID identifies an isolated tenant.
type TenantID string

// ErrNotFound indicates that the requested object does not exist.
var ErrNotFound = errors.New("port: not found")

// ErrAlreadyExists indicates that the requested object already exists.
var ErrAlreadyExists = errors.New("port: already exists")

// ErrConflict indicates that an optimistic update lost a race.
var ErrConflict = errors.New("port: conflict")

// ErrUnauthorized indicates that the caller lacks access to an object.
var ErrUnauthorized = errors.New("port: unauthorized")
