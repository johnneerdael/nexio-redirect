package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReleaseRedirectsToLatestStableReleaseApk(t *testing.T) {
	server := newGitHubFixture(t, []githubRelease{
		release("v0.58", true, "2026-05-12T10:00:00Z", asset("nexio-earlyaccess.apk", "https://github.com/johnneerdael/nexio/releases/download/v0.58/nexio-earlyaccess.apk")),
		release("v0.55", false, "2026-05-10T10:00:00Z", asset("nexio-release.apk", "https://github.com/johnneerdael/nexio/releases/download/v0.55/nexio-release.apk")),
	})
	defer server.Close()

	handler := newTestHandler(server.URL)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/release", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusFound, recorder.Body.String())
	}
	if got, want := recorder.Header().Get("Location"), "https://github.com/johnneerdael/nexio/releases/download/v0.55/nexio-release.apk"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestPreReleaseRedirectsToLatestPrereleaseWhenNewerThanStable(t *testing.T) {
	server := newGitHubFixture(t, []githubRelease{
		release("v0.58", true, "2026-05-12T10:00:00Z", asset("nexio-earlyaccess.apk", "https://github.com/johnneerdael/nexio/releases/download/v0.58/nexio-earlyaccess.apk")),
		release("v0.55", false, "2026-05-10T10:00:00Z", asset("nexio-release.apk", "https://github.com/johnneerdael/nexio/releases/download/v0.55/nexio-release.apk")),
	})
	defer server.Close()

	handler := newTestHandler(server.URL)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pre-release", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusFound, recorder.Body.String())
	}
	if got, want := recorder.Header().Get("Location"), "https://github.com/johnneerdael/nexio/releases/download/v0.58/nexio-earlyaccess.apk"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestPreReleaseFallsBackToLatestStableEarlyAccessAssetWhenStableIsNewer(t *testing.T) {
	server := newGitHubFixture(t, []githubRelease{
		release("v0.60", false, "2026-05-13T10:00:00Z",
			asset("nexio-release.apk", "https://github.com/johnneerdael/nexio/releases/download/v0.60/nexio-release.apk"),
			asset("nexio-earlyaccess.apk", "https://github.com/johnneerdael/nexio/releases/download/v0.60/nexio-earlyaccess.apk"),
		),
		release("v0.58", true, "2026-05-12T10:00:00Z", asset("nexio-earlyaccess.apk", "https://github.com/johnneerdael/nexio/releases/download/v0.58/nexio-earlyaccess.apk")),
	})
	defer server.Close()

	handler := newTestHandler(server.URL)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pre-release", nil))

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusFound, recorder.Body.String())
	}
	if got, want := recorder.Header().Get("Location"), "https://github.com/johnneerdael/nexio/releases/download/v0.60/nexio-earlyaccess.apk"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestMissingSelectedAssetReturnsBadGateway(t *testing.T) {
	server := newGitHubFixture(t, []githubRelease{
		release("v0.60", false, "2026-05-13T10:00:00Z", asset("nexio-release.apk", "https://github.com/johnneerdael/nexio/releases/download/v0.60/nexio-release.apk")),
		release("v0.58", true, "2026-05-12T10:00:00Z", asset("different.apk", "https://example.test/different.apk")),
	})
	defer server.Close()

	handler := newTestHandler(server.URL)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/pre-release", nil))

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if body := recorder.Body.String(); body == "" || body == "ok" {
		t.Fatalf("expected explanatory error body, got %q", body)
	}
}

func TestHealthzReturnsOK(t *testing.T) {
	handler := newTestHandler("https://api.github.invalid")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got, want := recorder.Body.String(), "ok\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func newTestHandler(apiBaseURL string) http.Handler {
	client := &githubClient{
		httpClient: http.DefaultClient,
		apiBaseURL: apiBaseURL,
		owner:      "johnneerdael",
		repo:       "nexio",
	}
	return newServer(client, time.Minute)
}

func newGitHubFixture(t *testing.T, releases []githubRelease) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/johnneerdael/nexio/releases" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Fatalf("encode releases: %v", err)
		}
	}))
}

func release(tag string, prerelease bool, publishedAt string, assets ...githubAsset) githubRelease {
	parsed, err := time.Parse(time.RFC3339, publishedAt)
	if err != nil {
		panic(err)
	}
	return githubRelease{
		TagName:     tag,
		Prerelease:  prerelease,
		PublishedAt: parsed,
		Assets:      assets,
	}
}

func asset(name string, url string) githubAsset {
	return githubAsset{Name: name, BrowserDownloadURL: url}
}
