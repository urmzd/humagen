Yes. `Deliver` retries three times, with a backoff that doubles from 200 ms. The limit is set in [`internal/retry/retry.go:18`](https://github.com/acme/webhooks/blob/3f9c2a7e1b4d5c6a8e9f0a1b2c3d4e5f6a7b8c9d/internal/retry/retry.go#L18).

To allow more attempts, pass `WithMaxAttempts` when you build the client:

```go
client := webhooks.New(webhooks.WithMaxAttempts(5))
```

<sub>[View the session in Humagen ↗](https://humagen.example/sessions/s_4k2)</sub>
