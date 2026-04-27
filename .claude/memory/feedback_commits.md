---
name: Commit message style
description: Keep commit messages short — one line, no body, no multi-line format
type: feedback
originSessionId: c1e0e7a4-2fb5-4643-9ce3-e095b4155be7
---
Use a single short imperative subject line. No body, no bullet points, no multi-line messages. Never add `Co-Authored-By: Claude ...` trailers.

**Why:** User corrected a verbose multi-paragraph message; global CLAUDE.md explicitly forbids mentioning Claude in commit messages.

**How to apply:** `git commit -m "Short description"` — no heredoc, no multi-line format, no co-author trailers.
