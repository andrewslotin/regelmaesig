---
name: Commit message style
description: Keep commit messages short — one line, no body, no multi-line format
type: feedback
originSessionId: c1e0e7a4-2fb5-4643-9ce3-e095b4155be7
---
Use a single short imperative subject line. No body, no bullet points, no multi-line messages.

**Why:** User explicitly corrected a verbose multi-paragraph commit message.

**How to apply:** `git commit -m "Short description"` — never use heredoc or multi-line format unless the user asks for it.
