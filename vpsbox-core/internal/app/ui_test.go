package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testDashboardPort = 7878

func guardedTestHandler(t *testing.T) (*Server, http.Handler, *bool) {
	t.Helper()
	server := NewServer(nil)
	reached := false
	handler := server.guard(testDashboardPort, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	return server, handler, &reached
}

func dashboardPost(host, origin, token string) *http.Request {
	form := url.Values{}
	if token != "" {
		form.Set("token", token)
	}
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/instances/dev-1/destroy", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

func TestNewServerMintsADistinctToken(t *testing.T) {
	a, b := NewServer(nil), NewServer(nil)
	if len(a.token) < 20 || a.token == b.token {
		t.Fatalf("tokens %q and %q are not distinct, unguessable values", a.token, b.token)
	}
}

func TestDashboardGuardAcceptsItsOwnPage(t *testing.T) {
	for _, host := range []string{"127.0.0.1:7878", "localhost:7878"} {
		server, handler, reached := guardedTestHandler(t)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, dashboardPost(host, "http://"+host, server.token))
		if rec.Code != http.StatusNoContent || !*reached {
			t.Fatalf("POST from %s with the page token = %d, want it to reach the handler", host, rec.Code)
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("dashboard responses must refuse framing")
		}
	}
}

func TestDashboardGuardRefusesCrossSiteAndRebindingRequests(t *testing.T) {
	server := NewServer(nil)
	cases := map[string]*http.Request{
		// A form on any website can post here, but can't read the token.
		"cross-site form without token": dashboardPost("127.0.0.1:7878", "https://evil.example", ""),
		"cross-site form with a guess":  dashboardPost("127.0.0.1:7878", "https://evil.example", "guess"),
		// Even a leaked token is refused from another origin.
		"foreign origin with token": dashboardPost("127.0.0.1:7878", "https://evil.example", server.token),
		"no origin, no token":       dashboardPost("127.0.0.1:7878", "", ""),
		// DNS rebinding: the browser talks to 127.0.0.1 but names the attacker's host.
		"rebinding host": dashboardPost("attacker.example:7878", "http://attacker.example:7878", server.token),
		"wrong port":     dashboardPost("127.0.0.1:9999", "", server.token),
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			reached := false
			handler := server.guard(testDashboardPort, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
			}))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden || reached {
				t.Fatalf("status = %d, reached handler = %v; want 403 and no handler call", rec.Code, reached)
			}
		})
	}
}

func TestDashboardGuardRefusesRebindingReads(t *testing.T) {
	_, handler, reached := guardedTestHandler(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://attacker.example:7878/", nil))
	if rec.Code != http.StatusForbidden || *reached {
		t.Fatalf("GET with a foreign Host = %d, want 403: the page carries the token", rec.Code)
	}
}

func TestDashboardPageEmbedsTheTokenInEveryForm(t *testing.T) {
	rec := httptest.NewRecorder()
	render(rec, pageData{Token: "page-token-123"})
	body := rec.Body.String()

	forms := strings.Count(body, "<form ")
	tokens := strings.Count(body, `name="token" value="page-token-123"`)
	if forms == 0 || forms != tokens {
		t.Fatalf("page has %d forms but %d token fields", forms, tokens)
	}
}
