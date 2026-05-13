package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultOwner      = "johnneerdael"
	defaultRepo       = "nexio"
	defaultAPIBaseURL = "https://api.github.com"
	releaseAssetName  = "nexio-release.apk"
	earlyAssetName    = "nexio-earlyaccess.apk"
)

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt time.Time     `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubClient struct {
	httpClient *http.Client
	apiBaseURL string
	owner      string
	repo       string
	token      string
}

func (c *githubClient) releases(ctx context.Context) ([]githubRelease, error) {
	baseURL := strings.TrimRight(c.apiBaseURL, "/")
	url := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=30", baseURL, c.owner, c.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "nexio-github-redirector")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github releases request failed: %s", resp.Status)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].PublishedAt.After(releases[j].PublishedAt)
	})
	return releases, nil
}

type releaseSource interface {
	releases(context.Context) ([]githubRelease, error)
}

type cachedResolver struct {
	source releaseSource
	ttl    time.Duration

	mu        sync.Mutex
	fetchedAt time.Time
	releases  []githubRelease
}

func (r *cachedResolver) releaseURL(ctx context.Context) (string, error) {
	stable, _, err := r.latestStableAndPrerelease(ctx)
	if err != nil {
		return "", err
	}
	if stable == nil {
		return "", errors.New("no stable GitHub release found")
	}
	return assetURL(*stable, releaseAssetName)
}

func (r *cachedResolver) earlyAccessURL(ctx context.Context) (string, error) {
	stable, prerelease, err := r.latestStableAndPrerelease(ctx)
	if err != nil {
		return "", err
	}
	if prerelease == nil && stable == nil {
		return "", errors.New("no GitHub releases found")
	}

	selected := prerelease
	if stable != nil && (prerelease == nil || stable.PublishedAt.After(prerelease.PublishedAt)) {
		selected = stable
	}
	if selected == nil {
		return "", errors.New("no release selected for early access APK")
	}
	return assetURL(*selected, earlyAssetName)
}

func (r *cachedResolver) latestStableAndPrerelease(ctx context.Context) (*githubRelease, *githubRelease, error) {
	releases, err := r.getReleases(ctx)
	if err != nil {
		return nil, nil, err
	}

	var stable *githubRelease
	var prerelease *githubRelease
	for i := range releases {
		release := releases[i]
		if release.Prerelease {
			if prerelease == nil || release.PublishedAt.After(prerelease.PublishedAt) {
				prerelease = &release
			}
			continue
		}
		if stable == nil || release.PublishedAt.After(stable.PublishedAt) {
			stable = &release
		}
	}
	return stable, prerelease, nil
}

func (r *cachedResolver) getReleases(ctx context.Context) ([]githubRelease, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.releases) > 0 && time.Since(r.fetchedAt) < r.ttl {
		return r.releases, nil
	}
	releases, err := r.source.releases(ctx)
	if err != nil {
		return nil, err
	}
	r.releases = releases
	r.fetchedAt = time.Now()
	return releases, nil
}

func assetURL(release githubRelease, name string) (string, error) {
	for _, asset := range release.Assets {
		if asset.Name == name && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("%s unavailable on %s", name, release.TagName)
}

func newServer(source releaseSource, cacheTTL time.Duration) http.Handler {
	resolver := &cachedResolver{source: source, ttl: cacheTTL}
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/release", redirectHandler(resolver.releaseURL))
	mux.HandleFunc("/pre-release", redirectHandler(resolver.earlyAccessURL))
	return mux
}

func redirectHandler(resolve func(context.Context) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		url, err := resolve(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
	}
}

func main() {
	port := envOrDefault("PORT", "8080")
	cacheTTL, err := time.ParseDuration(envOrDefault("CACHE_TTL", "5m"))
	if err != nil {
		log.Fatalf("invalid CACHE_TTL: %v", err)
	}

	client := &githubClient{
		httpClient: &http.Client{Timeout: 15 * time.Second},
		apiBaseURL: envOrDefault("GITHUB_API_BASE_URL", defaultAPIBaseURL),
		owner:      envOrDefault("GITHUB_OWNER", defaultOwner),
		repo:       envOrDefault("GITHUB_REPO", defaultRepo),
		token:      os.Getenv("GITHUB_TOKEN"),
	}

	addr := ":" + port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, newServer(client, cacheTTL)); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
