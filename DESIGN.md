# http (Go) — Design

A direct HTTP/HTTPS client, for use as the plain-HTTP **protocol service**
by a future `1m5-core-go` — the clearnet counterpart to `tor-go` and
`i2p-go`. A Go port of `ra.http.HTTPService` in
[`http-java`](https://github.com/resolvingarchitecture/http-java),
scoped to the client (outbound `sendOut`) half only.

## Where it sits

    (future) 1m5-core-go  ──wraps──►  http.HTTPClient
                                              │
                                     net/http.Client
                                              │
                                    clearnet HTTP/HTTPS
                                    (or a socks5:///http:// proxy,
                                     e.g. a local Tor/I2P daemon)

## Not the Jetty half

`http-java`'s `HTTPService` is two things bolted together: an
outbound client (`sendOut`, built on OkHttp) *and* a local server framework
(`launch`, `EnvelopeHandler`, `SPAHandler`, `EnvelopeWebSocket`,
`EnvelopeJSONDataHandler`) used to host 1M5's own API/SPA and — via
`tor-client-java`'s `TORClientService extends HTTPService` — Tor hidden
services. No other language port has needed the server half yet:
`tor-client-{python,ts,rust,cpp,cs,go}` never grew server-hosting, only a
same-process inbound listener would need it, and none exists yet in any
non-Java `1m5-core`. So this port is client-only; the server half stays a
Java-only feature until a concrete non-Java consumer needs it.

## Why `net/http`, not hand-rolled sockets

`tor-go`'s `http.go` hand-rolls SOCKS5 + a raw `GET` on `net.Conn`,
deliberately: it goes through Tor's SOCKS proxy and originally needed no
TLS (see that package's `DESIGN.md`). This package has the opposite
default — full HTTP semantics (all four verbs, headers, HTTPS, redirects,
multipart bodies) are the actual job — so it uses the standard library's
`net/http`, which already does all of that correctly, including `socks5://`
proxy URLs (`http.Transport.Proxy`, built in since Go 1.10) — no extra
dependency needed either way. A `ProxyURL` field is exposed so
`tor-go`/`i2p-go` could later reuse this client with their SOCKS
proxy instead of maintaining their own minimal HTTP parsing — not wired up
by this change; see `TODO.md`.

## Components

    HTTPClient   config, status, Start()/Stop()/Send()
    BlockReport  a suspected network-level block inferred from a response
                 status code (403/408/410/418/451/511), mirroring
                 ra.http.HTTPService#handleFailure

Flat package `http` at the repo root, matching `tor-go`/
`i2p-go`/`seda-bus-go`/`service-bus-go`'s layout (no internal subpackages).

## Message flow

**Outbound** — a caller builds a `messaging.DocumentEnvelope()`, sets `URL`
and `ActionValue` (`Get`/`Post`/`Put`/`Delete`), optionally sets the
`Authorization`/`Content-Type`/`Content-Disposition`/
`Content-Transfer-Encoding`/`User-Agent` headers and a body via
`AddContent` (string or `[]byte`) or `Multipart`, and calls `Send`. The
response body lands on the same envelope via `AddContent`; a non-2xx
status appends the code to `ErrorMessages` and, for a known blocked-style
code, sets `LastBlock`.

Unlike `tor-go`'s ad hoc `Headers["url"]`/`Headers["body"]`/
`Headers["error"]` convention (a shortcut for its minimal SOCKS-only GET),
this port uses `Envelope`'s actual typed fields (`URL`, `ActionValue`,
`Content()`/`AddContent()`, `ErrorMessages()`) — the fuller surface
`ra.http.HTTPService#sendOut` uses, and the shape `TorProtocolService`/
`HttpProtocolService` expect in `1m5-core-java`.

**Inbound** — not implemented (no server half; see above).

## Status model

`Status` is its own small `int32`-backed type (`Disconnected`,
`Connecting`, `Connected`, `Error`), backed by `atomic.Int32` — same
pattern as `tor-go`/`i2p-go`, not `ra-common-go`'s wider
`servicestatus` types. `Send` lazily calls `Start` if not already
connected, matching `HTTPService#sendOut`'s
`if(!isConnected() && !connect())`.

## Identity metadata leaks

Required standard for any HTTP client this project relies on for anonymized
traffic (Tor/I2P), enforced here and checked against every sibling
`http-*` port: no default header, response header, or connection
behavior may reveal more about the requester than it has to.

- **Code changed 2026-09-26, ⚠ not yet build-verified**: this client used to
  set no explicit default `User-Agent`, leaving `net/http.Transport` free
  to inject its own `User-Agent: Go-http-client/1.1` on every request with
  none set (confirmed: no `User-Agent` handling existed in `client.go`
  outside the caller-supplied-header path). That's a real fingerprinting
  signal - it identifies the exact language runtime and HTTP stack to every
  destination and any on-path observer. Now `DefaultUserAgent` (a generic,
  widely-shared browser value) is sent whenever the caller hasn't supplied
  one - the same fix already applied to `http-java` (OkHttp's own
  default, confirmed via bytecode), `http-cpp`/`http-python`
  (both previously defaulted to the project-identifying literal
  `"ra-http-client"`, arguably worse), `http-rust`/`http-ts`,
  and `1m5-remnant`'s Android `TorClient`.
  **This one is unverified, unlike all of those**: no Go toolchain was
  available in the environment this change was made in - `go` was not on
  `PATH`. `go build`, `go vet`, and `go test ./...` have never actually
  been run against this change. Treat it as "code written," not "fix
  confirmed," until someone with a working Go toolchain runs the `Build`
  sequence in `README.md` and it comes back clean. See `TODO.md`'s
  standalone verification item - it's deliberately not bundled into the
  same checklist entry as the code change, so it can't be missed or
  mistaken for already done.
- **Not yet verified**: this repo's own comment claims `net/http.Transport`
  resolves `socks5://` proxy URLs "built in since Go 1.10," implying the
  destination hostname is handed to the SOCKS layer for remote resolution
  rather than resolved locally first - matching what `http-cpp`'s
  `ConnectThroughSocks5` was directly confirmed to do. That claim hasn't
  been independently re-verified against Go's actual stdlib source in this
  pass (unlike the C++ check, which was) - do that before relying on this
  client to route anything through `TorSocksRelay`. A local resolution would
  leak the destination outside the proxy entirely, the same bug found and
  fixed in `bitcoin-client-java`'s bitcoinj DNS-seed lookups
  (`tor-client-java`, 2026-09-25).
- **No server/inbound half** (see "Not the Jetty half" above), so the third
  known leak shape - a server-identifying response header, found and fixed
  in `http-java`'s Jetty listener (`Server: Jetty(<version>)`) -
  doesn't apply yet. Check for it if P2's local server hosting is ever built.

## Not here

- The Jetty-equivalent local server / SPA / WebSocket hosting side of
  `HTTPService` (see above).
- Redirect-following tuning, connection pool tuning beyond `net/http`
  defaults.
- Tor/I2P SOCKS proxy reuse by `tor-go`/`i2p-go` (the `ProxyURL`
  field makes it possible; not wired up here).
- A `NetworkConnectionReport`-style event stream — `BlockReport` is just
  the last one, read via `LastBlock`.
