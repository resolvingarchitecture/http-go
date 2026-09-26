// Package http is a direct (non-anonymized) HTTP/HTTPS client for
// 1M5, ported from http-client-java's ra.http.HTTPService — client (outbound
// sendOut) only; the Jetty-based local server/SPA/WebSocket hosting side of
// HTTPService is not ported (no Go equivalent need has come up yet, see
// DESIGN.md).
package http

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/resolvingarchitecture/ra-common-go/messaging"
)

// DefaultRequestTimeout is the default per-request timeout.
var DefaultRequestTimeout = 60 * time.Second

// DefaultUserAgent is sent whenever a caller's Envelope has no User-Agent header of its own.
// Without this, net/http.Transport injects its own "Go-http-client/1.1" on any request with
// none set - identifying the exact language runtime and HTTP stack to every destination and
// any on-path observer, a real fingerprinting signal. A generic, widely-shared value instead -
// deliberately not reflecting this library or its version - matches Tor Browser's own practice
// of giving every user an identical, unremarkable fingerprint. See DESIGN.md "Identity
// metadata leaks".
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:128.0) Gecko/20100101 Firefox/128.0"

// Status is HTTPClient's own small status vocabulary - not ra-common-go's
// wider servicestatus/network status types - matching every other port
// (tor-client-go, i2p-go).
type Status int32

const (
	StatusDisconnected Status = iota
	StatusConnecting
	StatusConnected
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusConnecting:
		return "Connecting"
	case StatusConnected:
		return "Connected"
	case StatusError:
		return "Error"
	default:
		return "Disconnected"
	}
}

// BlockReport records a suspected network-level block inferred from a
// response status code, mirroring ra.http.HTTPService#handleFailure.
type BlockReport struct {
	URL       string
	Code      string
	Reason    string
	Message   string
	Timestamp time.Time
}

func blockReasonFor(code int) (reason, message string, ok bool) {
	switch code {
	case 403:
		return "BLOCKED-FORBIDDEN", "Received HTTP 403 response: Forbidden. Request considered blocked.", true
	case 408:
		return "BLOCKED-TIMEOUT", "Received HTTP 408 response: Request Timeout. Request considered blocked.", true
	case 410:
		return "BLOCKED-GONE", "Received HTTP 410 response: Gone. Request considered blocked.", true
	case 418:
		return "BLOCKED-TEAPOT", "Received HTTP 418 response: I'm a teapot. Might be blocking.", true
	case 451:
		return "BLOCKED-LEGAL", "Received HTTP 451 response: unavailable for legal reasons. Request considered blocked.", true
	case 511:
		return "BLOCKED-AUTHN", "Received HTTP 511 response: network authentication required. Request considered blocked.", true
	default:
		return "", "", false
	}
}

// HTTPClient is a plain HTTP/HTTPS client driven by messaging.Envelope.
// Safe for concurrent use once Start has returned.
type HTTPClient struct {
	// TrustAllCerts skips TLS certificate verification - test-only, mirrors
	// ra.http.client.trustallcerts in http-client-java.
	TrustAllCerts bool
	// RequestTimeout bounds each request (connect + read).
	RequestTimeout time.Duration
	// ProxyURL, if set, routes requests through it. Go's net/http.Transport
	// understands "http", "https" and "socks5" schemes natively, so this
	// covers a future Tor/I2P SOCKS proxy reuse without an extra dependency.
	ProxyURL *url.URL

	// LastBlock is the most recently detected suspected block (nil if none).
	LastBlock *BlockReport

	client *http.Client
	status atomic.Int32
}

// NewHTTPClient returns an HTTPClient with default settings.
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{RequestTimeout: DefaultRequestTimeout}
}

// FromConfig builds an HTTPClient from config keys:
// ra.http.client.trustallcerts, ra.http.client.requestTimeoutSecs,
// ra.http.client.proxyURL.
func FromConfig(cfg map[string]string) *HTTPClient {
	c := NewHTTPClient()
	if v, ok := cfg["ra.http.client.trustallcerts"]; ok {
		c.TrustAllCerts = v == "true"
	}
	if v, ok := cfg["ra.http.client.requestTimeoutSecs"]; ok {
		if secs, err := strconv.ParseFloat(v, 64); err == nil {
			c.RequestTimeout = time.Duration(secs * float64(time.Second))
		}
	}
	if v, ok := cfg["ra.http.client.proxyURL"]; ok && v != "" {
		if u, err := url.Parse(v); err == nil {
			c.ProxyURL = u
		}
	}
	return c
}

func (c *HTTPClient) GetStatus() Status  { return Status(c.status.Load()) }
func (c *HTTPClient) setStatus(s Status) { c.status.Store(int32(s)) }
func (c *HTTPClient) IsConnected() bool  { return c.GetStatus() == StatusConnected }

// Start builds the underlying http.Client. Never fails - matches
// http-client-java's connect(), which only errors on TLS setup problems
// that TrustAllCerts=false never hits.
func (c *HTTPClient) Start() bool {
	c.setStatus(StatusConnecting)
	transport := &http.Transport{}
	if c.TrustAllCerts {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 - opt-in test-only escape hatch
	}
	if c.ProxyURL != nil {
		transport.Proxy = http.ProxyURL(c.ProxyURL)
	}
	c.client = &http.Client{Transport: transport, Timeout: c.RequestTimeout}
	c.setStatus(StatusConnected)
	return true
}

// Stop tears down the underlying http.Client.
func (c *HTTPClient) Stop() bool {
	c.client = nil
	c.setStatus(StatusDisconnected)
	return true
}

// Send builds an HTTP request from envelope (URL, Action, headers, body)
// and executes it, writing the response body onto envelope via AddContent.
// envelope must be a document envelope (messaging.DocumentEnvelope()) for
// AddContent to have anywhere to put the response - same requirement as
// http-client-java's Envelope.documentFactory(). Mirrors
// ra.http.HTTPService#sendOut.
func (c *HTTPClient) Send(envelope *messaging.Envelope) bool {
	if !c.IsConnected() && !c.Start() {
		envelope.AddErrorMessage("HTTP Client not connected and unable to connect.")
		return false
	}
	if envelope.URL == nil || *envelope.URL == "" {
		envelope.AddErrorMessage("Must provide a URL.")
		return false
	}
	method, ok := actionToMethod(envelope.ActionValue)
	if !ok {
		envelope.AddErrorMessage("Envelope.Action must be set to Post, Put, Delete, or Get")
		return false
	}

	contentType, _ := envelope.Header(messaging.HeaderContentType).(string)
	var body io.Reader
	if envelope.Multipart != nil {
		contentType = "multipart/form-data; boundary=" + envelope.Multipart.Boundary
		body = strings.NewReader(envelope.Multipart.Finish())
	} else if content := envelope.Content(); content != nil {
		switch v := content.(type) {
		case string:
			body = strings.NewReader(v)
		case []byte:
			body = bytes.NewReader(v)
		default:
			envelope.AddErrorMessage(fmt.Sprintf("unsupported content type %T for HTTP body", v))
			return false
		}
	}

	req, err := http.NewRequest(method, *envelope.URL, body)
	if err != nil {
		envelope.AddErrorMessage(err.Error())
		return false
	}
	if v, ok := envelope.Header(messaging.HeaderAuthorization).(string); ok && v != "" {
		req.Header.Set(messaging.HeaderAuthorization, v)
	}
	if contentType != "" {
		req.Header.Set(messaging.HeaderContentType, contentType)
	}
	if v, ok := envelope.Header(messaging.HeaderContentDisposition).(string); ok && v != "" {
		req.Header.Set(messaging.HeaderContentDisposition, v)
	}
	if v, ok := envelope.Header(messaging.HeaderContentTransferEncoding).(string); ok && v != "" {
		req.Header.Set(messaging.HeaderContentTransferEncoding, v)
	}
	if v, ok := envelope.Header(messaging.HeaderUserAgent).(string); ok && v != "" {
		req.Header.Set(messaging.HeaderUserAgent, v)
	} else {
		req.Header.Set(messaging.HeaderUserAgent, DefaultUserAgent)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		envelope.AddErrorMessage(err.Error())
		return false
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		envelope.AddErrorMessage(err.Error())
		return false
	}
	if !envelope.AddContent(respBody) {
		envelope.AddErrorMessage("envelope must be a document envelope (messaging.DocumentEnvelope()) to receive HTTP response content")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		envelope.AddErrorMessage(strconv.Itoa(resp.StatusCode))
		c.recordBlockIfKnown(*envelope.URL, resp.StatusCode)
		return false
	}
	return true
}

func actionToMethod(a *messaging.Action) (string, bool) {
	if a == nil {
		return "", false
	}
	switch *a {
	case messaging.ActionGet:
		return http.MethodGet, true
	case messaging.ActionPost:
		return http.MethodPost, true
	case messaging.ActionPut:
		return http.MethodPut, true
	case messaging.ActionDelete:
		return http.MethodDelete, true
	default:
		return "", false
	}
}

func (c *HTTPClient) recordBlockIfKnown(u string, code int) {
	reason, message, ok := blockReasonFor(code)
	if !ok {
		return
	}
	c.LastBlock = &BlockReport{URL: u, Code: strconv.Itoa(code), Reason: reason, Message: message, Timestamp: time.Now()}
	fmt.Fprintln(os.Stderr, "http-client: "+message)
}
