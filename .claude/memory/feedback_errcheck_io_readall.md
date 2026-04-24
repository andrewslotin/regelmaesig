---
name: Handle io.ReadAll errors in caching handlers
description: Never silently ignore io.ReadAll errors when buffering response bodies for caching; a dropped connection produces a truncated body that must not be cached or served
type: feedback
originSessionId: 7ad5d239-cda8-40a7-8669-73d6edff63dd
---
Always check the error from `io.ReadAll` when buffering a response body for caching. Silently discarding the error (`body, _ := io.ReadAll(...)`) means a mid-transfer connection drop produces a truncated body that gets stored in the cache and served to clients.

**Why:** Caught in code review on the regelmaesig caching feature (commit b95d913). `newStandardHandler` in proxy.go checked the error correctly; the two custom redirect handlers (shapes, maps) did not, and were fixed.

**How to apply:** Any time a response body is buffered with `io.ReadAll` as part of a write-to-cache path, treat the error as a failure and fall back to cache-miss / error response — same as a network error. Pattern:

```go
body, err := io.ReadAll(resp.Body)
if err != nil {
    // serve fallback, not the truncated body
}
```
