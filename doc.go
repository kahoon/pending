// Package pending provides a small in-memory scheduler for deferred tasks.
//
// Tasks are keyed by ID, so scheduling with the same ID replaces the previous
// task (debouncing). The manager supports cancellation, graceful shutdown, and
// optional concurrency limits with block or drop strategies.
//
// Use Schedule for the simple delayed-execution path, and ScheduleWith with
// ScheduleOptions for advanced behavior such as absolute-time scheduling and
// non-replacing IfAbsent semantics.
//
// Runtime state can be inspected with Stats(), while IsPending() and
// TimeRemaining() provide per-task introspection.
package pending
