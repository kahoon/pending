# Contributing to pending

Thanks for your interest in contributing.

## Development setup

1. Install Go `1.25+`.
2. Clone the repository.
3. Run:
   - `go test ./...`
   - `go test -race ./...`
   - `go test -cover ./...`

## Contribution guidelines

1. Keep changes focused and small.
2. Add or update tests for any behavioral change.
3. Update docs (`README.md` and GoDoc comments) for public API changes.
4. Keep the project dependency-free unless there is a strong justification.
5. Ensure task logic remains context-aware and concurrency-safe.

## Pull requests

1. Describe the problem and the change clearly.
2. Include any tradeoffs or limitations.
3. Confirm all checks pass locally.
4. Reference related issues when relevant.

## Reporting bugs

Please open a bug report using the provided issue template and include:
- Go version
- OS/architecture
- Minimal reproduction
- Expected vs actual behavior

