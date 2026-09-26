# http (Go)

A direct (non-anonymized) HTTP/HTTPS client for **1M5**: builds a request
from a `messaging.Envelope` (URL, action, headers, body) and writes the
response back onto it.

A Go port of the client (outbound `sendOut`) side of
[`http-java`](https://github.com/resolvingarchitecture/http-java)'s
`ra.http.HTTPService`. The Jetty-based local server / SPA / WebSocket
hosting side of `HTTPService` is not ported — no other language port has
needed it yet; see `DESIGN.md`.

## Use

```go
import (
    "github.com/resolvingarchitecture/ra-common-go/messaging"
    "github.com/resolvingarchitecture/http-go"
)

client := http.NewHTTPClient()   // or http.FromConfig(cfg)

env := messaging.DocumentEnvelope()    // must be a document envelope - AddContent needs it
url := "https://resolvingarchitecture.io"
env.URL = &url
action := messaging.ActionGet
env.ActionValue = &action

if client.Send(env) {                  // false (cleanly) on any failure
    body := env.Content().([]byte)     // response body
} else {
    _ = env.ErrorMessages()            // what went wrong
}
```

`Start()`/`Stop()` are optional — `Send` lazily connects on first use, same
as `ra.http.HTTPService#sendOut`.

### Config keys

| key | default | meaning |
|-----|---------|---------|
| `ra.http.client.trustallcerts` | `false` | skip TLS certificate verification (test-only) |
| `ra.http.client.requestTimeoutSecs` | `60` | per-request timeout |
| `ra.http.client.proxyURL` | unset | route requests through this proxy — `http://`, `https://`, or `socks5://` |

## Build

```
go build ./...
go test ./...
go vet ./...
gofmt -l .
```

Depends on `ra-common-go` via a `go.mod` `replace` directive pointing at
`../../common/ra-common-go`, matching every other Go port's
monorepo-dependency convention.

## Identity metadata leaks

**Code changed 2026-09-26, ⚠ not yet build-verified** - the same class of
bug found and fixed (and confirmed working) in
`http-java`/`http-cpp`/`http-python` and
`1m5-remnant`'s Android `TorClient`: this client used to set no explicit
default `User-Agent`, leaving Go's `net/http.Transport` free to inject its
own `User-Agent: Go-http-client/1.1` on every request with none set. Now
defaults to a generic, widely-shared browser value instead - but **no Go
toolchain was available in the environment this change was made in**
(`go` was not on `PATH`), so `go build`/`go vet`/`go test` have never
actually been run against it. The change is small and syntactically
ordinary Go, but "should compile" isn't "does compile" - run the full
`Build` sequence below and confirm it's clean before trusting this fix, and
before treating the corresponding `TODO.md` item as more than "code
written." See `DESIGN.md` "Identity metadata leaks" for the same caveat and
the still-open SOCKS5 DNS-resolution check, which also needs real
verification, not just assumed from `net/http`'s documented `socks5://`
support.

## Status

HTTP and HTTPS GET/POST/PUT/DELETE work, including multipart bodies and the
standard header set (`Authorization`, `Content-Type`,
`Content-Disposition`, `Content-Transfer-Encoding`, `User-Agent`). No
redirect following beyond `net/http`'s default, no connection pooling
tuning, no local server/SPA/WebSocket hosting. See `DESIGN.md` and
`TODO.md`.
