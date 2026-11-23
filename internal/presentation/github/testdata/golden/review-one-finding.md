<!-- event: REQUEST_CHANGES -->

**Needs changes: one problem to fix before merging.**

This change adds retries with backoff to webhook delivery. The loop now loses the final error, so a failed delivery looks like a success.

**1. Retry loop drops the final error**
<sub>Must fix · [`internal/retry/retry.go:45`](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L45) · found by code-reviewer</sub>

After the last failed attempt `Do` returns `nil`, so callers treat a failed delivery as a success. Return `lastErr` when the loop ends.

<details><summary>Evidence</summary>

- **1.** `Do` returns `nil` after the loop. ([source](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L45))

</details>

<!-- humagen.review.v2 verdict=request_changes errors=1 warnings=0 info=0 -->

<sub>[View the session in Humagen ↗](https://humagen.example/sessions/s_4k2)</sub>

<!-- line comment on internal/retry/retry.go:45 -->

**Retry loop drops the final error**
After the last failed attempt `Do` returns `nil`, so callers treat a failed delivery as a success. Return `lastErr` when the loop ends.

<sub>Must fix · found by code-reviewer</sub>

```suggestion
	return lastErr
```
