<!-- event: REQUEST_CHANGES -->

**Needs changes: one problem to fix before merging, and two more worth fixing.**

This change adds retries with backoff to webhook delivery and a token to the client. The loop loses the final error, which blocks the merge. The backoff and the debug log need a look too.

**1. Retry loop drops the final error**
<sub>Must fix · [`internal/retry/retry.go:45`](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L45) · found by code-reviewer</sub>
After the last failed attempt `Do` returns `nil`, so callers treat a failed delivery as a success. Return `lastErr` when the loop ends.

**2. Backoff ignores the context deadline**
<sub>Should fix · [`internal/retry/retry.go:43`](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L43) · found by qa-reviewer</sub>
`time.Sleep` keeps waiting after the caller cancels, so a shutdown can hang for the whole backoff. Wait on a timer and `ctx.Done()` in a `select`.

**3. The webhook token reaches the debug log**
<sub>Should fix · [`internal/webhooks/client.go:14`](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/webhooks/client.go#L14) · found by security-reviewer</sub>
The client logs itself with `slog.Any`, which prints every field, the new `token` among them. Give `Client` a `LogValue` method that leaves the token out.

**4. Backoff has no upper bound**
<sub>Note · [`internal/retry/retry.go:48`](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L48) · found by code-reviewer</sub>
Cap the delay, for example at five seconds.

**5. The limit field is never read**
<sub>Note · [`internal/webhooks/client.go:13`](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/webhooks/client.go#L13) · found by architect-reviewer</sub>
Use `limit` in `Deliver` or remove it.

<details><summary>All 5 findings</summary>

| # | Finding | Severity | Where |
| ---: | :--- | :--- | :--- |
| 1 | Retry loop drops the final error | Must fix | `internal/retry/retry.go:45` |
| 2 | Backoff ignores the context deadline | Should fix | `internal/retry/retry.go:43` |
| 3 | The webhook token reaches the debug log | Should fix | `internal/webhooks/client.go:14` |
| 4 | Backoff has no upper bound | Note | `internal/retry/retry.go:48` |
| 5 | The limit field is never read | Note | `internal/webhooks/client.go:13` |

</details>

<details><summary>Evidence</summary>

**1. Retry loop drops the final error**
- `Do` returns `nil` after the loop. ([source](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L45))

**2. Backoff ignores the context deadline**
- The wait is a bare `time.Sleep`. ([source](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L43))

**3. The webhook token reaches the debug log**
- `Deliver` logs the whole client at debug level. ([source](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/webhooks/client.go#L14))

**4. Backoff has no upper bound**
- `base << n` doubles without a cap. ([source](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L48))

**5. The limit field is never read**
- Nothing reads `Client.limit`. (`internal/webhooks/client.go:13`)

</details>

<!-- humagen.review.v2 verdict=request_changes errors=1 warnings=2 info=2 -->

<sub>[View the session in Humagen ↗](https://humagen.example/sessions/s_4k2)</sub>

<!-- line comment on internal/retry/retry.go:45 -->

**Retry loop drops the final error**
After the last failed attempt `Do` returns `nil`, so callers treat a failed delivery as a success. Return `lastErr` when the loop ends.

<sub>Must fix · found by code-reviewer</sub>

```suggestion
	return lastErr
```

<!-- line comment on internal/retry/retry.go:43 -->

**Backoff ignores the context deadline**
`time.Sleep` keeps waiting after the caller cancels, so a shutdown can hang for the whole backoff. Wait on a timer and `ctx.Done()` in a `select`.

<sub>Should fix · found by qa-reviewer</sub>

<!-- line comment on internal/webhooks/client.go:14 -->

**The webhook token reaches the debug log**
The client logs itself with `slog.Any`, which prints every field, the new `token` among them. Give `Client` a `LogValue` method that leaves the token out.

<sub>Should fix · found by security-reviewer</sub>

<!-- line comment on internal/retry/retry.go:48 -->

**Backoff has no upper bound**
Cap the delay, for example at five seconds.

<sub>Note · found by code-reviewer</sub>

<!-- line comment on internal/webhooks/client.go:13 -->

**The limit field is never read**
Use `limit` in `Deliver` or remove it.

<sub>Note · found by architect-reviewer</sub>
