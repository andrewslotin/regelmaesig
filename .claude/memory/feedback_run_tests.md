---
name: Run tests and build commands
description: User expects Claude to run build/test verification commands directly rather than asking the user to run them
type: feedback
originSessionId: c1e0e7a4-2fb5-4643-9ce3-e095b4155be7
---
Run `go build ./...`, `go vet ./...`, `golangci-lint run`, and `go test ./...` after implementing changes — don't ask the user to run them.

**Why:** The project CLAUDE.md says "Never try to run the code, ask user instead" but the user explicitly corrected this. They want verification steps executed automatically.

**How to apply:** After any implementation, run the verification commands (build, vet, test) before reporting the task as done.
