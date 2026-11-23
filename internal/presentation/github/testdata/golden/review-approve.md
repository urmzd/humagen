<!-- event: COMMENT -->

**Looks good to merge. One note below.**

This change adds retries with backoff to webhook delivery. Errors and cancellation are handled, and the new tests cover three failures in a row.

**1. Backoff has no upper bound**
<sub>Note · [`internal/retry/retry.go:48`](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L48) · found by code-reviewer</sub>

Cap the delay, for example at five seconds.

<details><summary>Evidence</summary>

- **1.** `base << n` doubles without a cap. ([source](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L48))

</details>

<!-- humagen.review.v2 verdict=approve errors=0 warnings=0 info=1 -->

<sub>[View the session in Humagen ↗](https://humagen.example/sessions/s_4k2)</sub>

<!-- line comment on internal/retry/retry.go:48 -->

**Backoff has no upper bound**
Cap the delay, for example at five seconds.

<sub>Note · found by code-reviewer</sub>
