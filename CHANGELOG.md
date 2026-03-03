# Changelog

All notable changes to this project will be documented in this file.

The format is based on https://keepachangelog.com/en/1.1.0/, and this project follows https://semver.org/spec/v2.0.0.html.

## [Unreleased]

## [0.1.1] - 2026-03-03

### Added
- Public sentinel error `ErrTaskDropped` for telemetry classification.
- Package-level docs and example for `pkg.go.dev`.
- Baseline benchmark suite for schedule, reschedule, cancel, and shutdown paths.
- README benchmark section with run command and sample output.
- `Stats()` snapshot API with pending/running/closed state.

### Changed
- Safer cleanup semantics for rescheduled tasks (`deleteIfCurrent`).
- Cancellation-aware `StrategyBlock` acquire path.
- Improved `OnExecuted` timing accuracy by measuring task duration before internal cleanup.
- README clarity and structure improvements.

### Fixed
- Prevented stale task execution cleanup from deleting newer entries with the same ID.

## [0.1.0] - 2026-02-28

### Added
- Initial release of `pending`.
- Deferred task scheduling by ID.
- Debounce-by-replace behavior.
- Cancellation and graceful shutdown APIs.
- Concurrency limiting with `StrategyBlock` and `StrategyDrop`.
- Telemetry hooks for scheduling lifecycle events.
