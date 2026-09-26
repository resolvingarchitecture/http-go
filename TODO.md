# http (Go) — TODO

## P0 — client (done)

- [x] `HTTPClient`: `NewHTTPClient`/`FromConfig`/`Start`/`Stop`/`Send`.
- [x] GET/POST/PUT/DELETE from `Envelope.ActionValue`.
- [x] Standard headers: `Authorization`, `Content-Type`,
      `Content-Disposition`, `Content-Transfer-Encoding`, `User-Agent`.
- [x] HTTPS via `net/http` + `crypto/tls` (real cert verification by
      default; `TrustAllCerts` escape hatch for tests).
- [x] Multipart body support via `ra-common-go/multipart`.
- [x] Blocked-status-code detection (403/408/410/418/451/511) →
      `BlockReport`, mirroring `HTTPService#handleFailure`.
- [x] `ProxyURL` (http/https/socks5) config key.
- [x] Tests against `httptest.Server`/`httptest.NewTLSServer` (no live
      network required); one live-network smoke test that skips cleanly
      if unreachable.

## P1 — request path

- [x] **Identity metadata leak, code fixed 2026-09-26**: no default
      `User-Agent` was set, so `net/http.Transport` injected its own
      `Go-http-client/1.1` - see DESIGN.md "Identity metadata leaks". Now
      sends `DefaultUserAgent` (generic) when the caller hasn't supplied one.
- [ ] **⚠ Verification still required, not done**: the fix above was written
      and committed in an environment with **no Go toolchain available at
      all** (`go` was not on `PATH`, confirmed) - `go build`, `go vet`, and
      `go test ./...` have **never been run** against it. The change is
      small and syntactically ordinary Go (a new `const` + an `if`/`else`
      branch on an existing `req.Header.Set` call), so it's low-risk, but
      "looks right" is not the same as "compiles and passes." Before this
      client is used anywhere for real: run the full `go build && go vet
      ./... && go test ./...` sequence from `README.md`'s Build section and
      confirm it's clean. Do this before checking the item above off as
      trustworthy, not just present.
- [ ] **Verify, don't just cite the doc comment**: confirm `net/http`'s
      `socks5://` proxy support genuinely resolves the destination hostname
      via the proxy, not local DNS, before this client (or `tor-go`
      reusing it) is trusted to route anything through `TorSocksRelay` - see
      DESIGN.md "Identity metadata leaks".
- [ ] Wire `tor-go`/`i2p-go` to use this client's `ProxyURL` instead
      of their own minimal HTTP parsing over a raw SOCKS `net.Conn`.
- [ ] Expose response status code + headers on the envelope (currently only
      the body and, on failure, the status code as an error message string).
- [ ] `context.Context`-aware variant (`SendCtx`) once a real caller needs
      cancellation beyond the flat `RequestTimeout`.
- [ ] Streaming request/response bodies (currently buffers both fully).

## P2 — local server / SPA / WebSocket hosting

- [ ] Not started — no non-Java consumer needs it yet. See `DESIGN.md`.
      Would need `EnvelopeHandler`/`SPAHandler`/`EnvelopeWebSocket`
      equivalents and a decision on which Go HTTP server story
      (`net/http` alone is probably enough — no Jetty-equivalent framework
      needed).

## Testing / ops

- [ ] CI: `go build`, `go vet`, `go test -race`, `gofmt -l`.
- [ ] Tag a release once the API settles (currently `replace`-directive
      dependency only).

## Cross-repo

- [ ] Wire into a future `1m5-core-go`'s protocol-service adapter, same
      pattern as `NetworkServiceProtocol`/`HttpProtocolService` in
      `1m5-core-java` (`1m5-core-rust`'s `protocol.rs` has `i2p`/`tor`
      features but no `http` one yet either).
- [ ] Port `http-{python,ts,rust,cpp,cs}` alongside this one (see
      the sibling repos under `http/`).
