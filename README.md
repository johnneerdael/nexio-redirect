# Nexio GitHub APK Redirector

Small Dockerized web server for stable Nexio download URLs.

## Routes

- `GET /release` redirects to the latest non-prerelease GitHub asset named `nexio-release.apk`.
- `GET /pre-release` redirects to the latest prerelease GitHub asset named `nexio-earlyaccess.apk`.
- If the latest stable release is newer than the latest prerelease, `/pre-release` redirects to `nexio-earlyaccess.apk` on the latest stable release.
- `GET /healthz` returns `ok`.

## Run Locally

```bash
go test ./...
go run .
```

Open:

```bash
curl -I http://127.0.0.1:8080/release
curl -I http://127.0.0.1:8080/pre-release
```

## Docker

```bash
docker build -t nexio-github-redirector .
docker run --rm -p 8080:8080 nexio-github-redirector
```

Set `GITHUB_TOKEN` to raise GitHub API rate limits:

```bash
docker run --rm -p 8080:8080 -e GITHUB_TOKEN=github_pat_xxx nexio-github-redirector
```

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port inside the container. |
| `CACHE_TTL` | `5m` | Duration to cache GitHub release metadata. |
| `GITHUB_OWNER` | `johnneerdael` | GitHub repository owner. |
| `GITHUB_REPO` | `nexio` | GitHub repository name. |
| `GITHUB_API_BASE_URL` | `https://api.github.com` | Override for tests or proxies. |
| `GITHUB_TOKEN` | empty | Optional GitHub API bearer token. |

## Caddy

Example reverse proxy for `https://download.nexioapp.org`:

```caddyfile
download.nexioapp.org {
	reverse_proxy github-redirector:8080
}
```

The public URLs become:

- `https://download.nexioapp.org/release`
- `https://download.nexioapp.org/pre-release`
# nexio-redirect
