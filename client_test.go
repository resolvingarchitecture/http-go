package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/resolvingarchitecture/ra-common-go/messaging"
)

func getAction() *messaging.Action {
	a := messaging.ActionGet
	return &a
}

func postAction() *messaging.Action {
	a := messaging.ActionPost
	return &a
}

func TestSendWithoutURLErrors(t *testing.T) {
	c := NewHTTPClient()
	env := messaging.DocumentEnvelope()
	env.ActionValue = getAction()
	if c.Send(env) {
		t.Fatal("expected Send to fail")
	}
	if len(env.ErrorMessages()) == 0 {
		t.Fatal("expected an error message")
	}
}

func TestSendWithoutActionErrors(t *testing.T) {
	c := NewHTTPClient()
	env := messaging.DocumentEnvelope()
	url := "http://example.invalid/"
	env.URL = &url
	if c.Send(env) {
		t.Fatal("expected Send to fail")
	}
}

func TestGetAgainstLocalServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("got method %s", r.Method)
		}
		if r.Header.Get(messaging.HeaderUserAgent) != "ra-http-client-test" {
			t.Errorf("got User-Agent %q", r.Header.Get(messaging.HeaderUserAgent))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer srv.Close()

	c := NewHTTPClient()
	env := messaging.DocumentEnvelope()
	url := srv.URL + "/test"
	env.URL = &url
	env.ActionValue = getAction()
	env.SetHeader(messaging.HeaderUserAgent, "ra-http-client-test")

	if !c.Send(env) {
		t.Fatalf("expected Send to succeed, errors=%v", env.ErrorMessages())
	}
	body, ok := env.Content().([]byte)
	if !ok {
		t.Fatalf("expected []byte content, got %T", env.Content())
	}
	if string(body) != "<html><body>ok</body></html>" {
		t.Fatalf("got %q", body)
	}
	if c.GetStatus() != StatusConnected {
		t.Fatalf("got status %v", c.GetStatus())
	}
}

func TestPostWithBodyAndContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("got method %s", r.Method)
		}
		if ct := r.Header.Get(messaging.HeaderContentType); ct != "application/json" {
			t.Errorf("got Content-Type %q", ct)
		}
		got, _ := io.ReadAll(r.Body)
		if string(got) != `{"a":1}` {
			t.Errorf("got body %q", got)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	defer srv.Close()

	c := NewHTTPClient()
	env := messaging.DocumentEnvelope()
	url := srv.URL + "/create"
	env.URL = &url
	env.ActionValue = postAction()
	env.SetHeader(messaging.HeaderContentType, "application/json")
	env.AddContent(`{"a":1}`)

	if !c.Send(env) {
		t.Fatalf("expected Send to succeed, errors=%v", env.ErrorMessages())
	}
	if string(env.Content().([]byte)) != "created" {
		t.Fatalf("got %q", env.Content())
	}
}

func TestBlockedStatusCodeRecordsBlockReport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewHTTPClient()
	env := messaging.DocumentEnvelope()
	url := srv.URL + "/blocked"
	env.URL = &url
	env.ActionValue = getAction()

	if c.Send(env) {
		t.Fatal("expected Send to fail on 403")
	}
	if c.LastBlock == nil || c.LastBlock.Reason != "BLOCKED-FORBIDDEN" {
		t.Fatalf("got LastBlock=%+v", c.LastBlock)
	}
}

func TestTrustAllCertsAcceptsSelfSignedServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secure"))
	}))
	defer srv.Close()

	c := NewHTTPClient()
	c.TrustAllCerts = true
	env := messaging.DocumentEnvelope()
	url := srv.URL + "/secure"
	env.URL = &url
	env.ActionValue = getAction()

	if !c.Send(env) {
		t.Fatalf("expected Send to succeed with TrustAllCerts, errors=%v", env.ErrorMessages())
	}
	if string(env.Content().([]byte)) != "secure" {
		t.Fatalf("got %q", env.Content())
	}
}

func TestFromConfigAppliesKeys(t *testing.T) {
	c := FromConfig(map[string]string{
		"ra.http.client.trustallcerts":      "true",
		"ra.http.client.requestTimeoutSecs": "5",
	})
	if !c.TrustAllCerts {
		t.Fatal("expected TrustAllCerts=true")
	}
	if c.RequestTimeout != 5*time.Second {
		t.Fatalf("got RequestTimeout=%v", c.RequestTimeout)
	}
}

// TestLiveHTTPSGet is a smoke test against the public internet, mirroring
// http-client-java's HTTPServiceTest#httpsClientTest. Skips cleanly (not a
// failure) if the network is unreachable, matching this repo's no-live-
// network-required policy for `go test ./...`.
func TestLiveHTTPSGet(t *testing.T) {
	c := NewHTTPClient()
	c.RequestTimeout = 5 * time.Second
	env := messaging.DocumentEnvelope()
	url := "https://resolvingarchitecture.dev"
	env.URL = &url
	env.ActionValue = getAction()

	if !c.Send(env) {
		errs := env.ErrorMessages()
		t.Skipf("live network unreachable, skipping: %v", errs)
	}
	body, _ := env.Content().([]byte)
	if len(body) == 0 {
		t.Fatal("expected a non-empty response body")
	}
}
