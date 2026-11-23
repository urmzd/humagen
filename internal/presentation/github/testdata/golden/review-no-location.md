<!-- event: COMMENT -->

**One problem worth fixing before merging.**

This change makes webhook delivery retry three times. The code is fine; one page of the docs is now out of date.

**1. The docs still say two retries**
<sub>Should fix · found by code-reviewer</sub>
`docs/reference/webhooks.md` says a delivery is retried twice, and this change makes it three times. Update the retry count and the backoff in that page.

<details><summary>Evidence</summary>

**1. The docs still say two retries**
- The page says deliveries retry twice. ([source](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/docs/reference/webhooks.md#L31))

</details>

<!-- humagen.review.v2 verdict=comment errors=0 warnings=1 info=0 -->

<sub>[View the session in Humagen ↗](https://humagen.example/sessions/s_4k2)</sub>
