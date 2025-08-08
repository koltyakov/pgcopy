# Quality & Security

## Testing
- Unit tests cover core logic (copy plan, progress, FK helpers)
- Integration tests validate end-to-end flows

## Lint and security
- golangci-lint enforces style and common pitfalls
- gosec (optional) for security checks

## CI
- GitHub Actions: build, test, lint, and release pipelines

## Logging & errors
- Structured logs for diagnosing issues
- Clear error reporting with actionable messages
