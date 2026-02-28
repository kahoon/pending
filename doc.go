// Package pending provides a small in-memory scheduler for deferred tasks.
//
// Tasks are keyed by ID, so scheduling with the same ID replaces the previous
// task (debouncing). The manager supports cancellation, graceful shutdown, and
// optional concurrency limits with block or drop strategies.
package pending

