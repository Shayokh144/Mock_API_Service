# Mock API proxy

A local Go proxy for one Flutter developer on a MacBook.

Point the debug build at `http://localhost:8080`. A matching rule returns a saved response. Every other call goes to the Ruby develop server. The Ruby server stays unchanged. Production app configuration stays unchanged.

Behavior, matching, and deletion rules live in [docs/MVP_REQUIREMENTS.md](docs/MVP_REQUIREMENTS.md). The build order is in [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md). The task list is in [docs/TASKS.md](docs/TASKS.md).

## Prerequisites

- macOS
- Go 1.22 or newer. This module was tested with Go 1.22.2.
- A Ruby develop server the Mac can already reach
- A Flutter app whose debug base URL you can change

Install Go from [https://go.dev/dl/](https://go.dev/dl/). Homebrew also works: `brew install go`.

## Install

```sh
git clone https://github.com/Shayokh144/Mock_API_Service.git
cd Mock_API_Service
go test ./...
```

Run commands from the repository root. The default rules file is `data/rules.json` in that directory.

## Configuration

The process reads only environment variables. It does not take flags, and it does not store Ruby credentials.

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `UPSTREAM_BASE_URL` | yes | none | Absolute `http` or `https` URL of the Ruby server. A path prefix is allowed. A query or fragment is rejected. |
| `LISTEN_ADDR` | no | `127.0.0.1:8080` | Loopback address. `localhost` becomes `127.0.0.1`. `[::1]` is allowed. Any other host fails startup. |
| `RULES_PATH` | no | `data/rules.json` | Rule file. A missing file means there are no rules. |
| `HISTORY_LIMIT` | no | `500` | Newest history entries kept in memory. |
| `UPSTREAM_TIMEOUT` | no | `30s` | Time allowed for one upstream call. |

```sh
export UPSTREAM_BASE_URL='https://ruby-dev.example.com'
```

A path prefix is joined in front of the request path. A trailing slash on the request path stays.

`https://ruby-dev.example.com` plus `GET /v1/users?page=2` becomes `GET https://ruby-dev.example.com/v1/users?page=2`.

## Run on macOS

```sh
go run ./cmd/mockproxy
```

The log prints:

```text
listening on 127.0.0.1:8080
dashboard at http://localhost:8080/_admin
```

Open [http://localhost:8080/_admin](http://localhost:8080/_admin). Stop the process with Ctrl+C.

Rules are saved when you create, edit, enable, disable, or delete them. They are still there after a restart. History is memory only. A restart clears history and keeps rules.

`data/rules.json` is ignored by git. `data/.gitkeep` stays in the repository.

## Connect a Flutter debug build

Change only the debug base URL.

- iOS Simulator and Flutter desktop on the same Mac: `http://localhost:8080`
- Android emulator: `http://10.0.2.2:8080`

Inside the Android emulator, `localhost` is the emulator itself. `10.0.2.2` is the Mac's loopback address. Leave the release host unchanged.

A physical phone cannot use `localhost`. That setup is outside this MVP.

The proxy forwards the method, path, query string, body, and ordinary headers. The Ruby app keeps its own auth. Do not put passwords in this repository.

## Sentry

Every mocked response includes `X-Mock-Response: true`. A passthrough response does not add that header. Proxy `413` and `502` responses do not add it either, so real local failures can still reach Sentry.

In the Flutter debug build, drop an event in `beforeSend` when the HTTP response header `X-Mock-Response` equals `true`. Header names are case-insensitive. The Dart types depend on the app's Sentry SDK version. Use the header contract with the response type that version exposes.

## Create a mock

Open the dashboard. Choose **New rule**.

Exact profile error:

| Field | Value |
| --- | --- |
| Name | Profile failure |
| Enabled | checked |
| Method | GET |
| Path pattern | `/api/v1/profile` |
| Status code | `500` |
| Headers | `Content-Type: application/json` |
| Body | `{"error":"planned failure"}` |

Call it:

```sh
curl -i 'http://127.0.0.1:8080/api/v1/profile?page=2'
```

The status is `500`. The body is the JSON you saved. The query string does not change which rule matches. The response includes `X-Mock-Response: true`.

Wildcard user:

| Field | Value |
| --- | --- |
| Method | GET |
| Path pattern | `/api/users/*` |
| Status code | `200` |
| Body | `{"id":"mocked"}` |

`*` is one whole path segment. It is not a regular expression.

```sh
curl -i 'http://127.0.0.1:8080/api/users/42'
```

`/api/users/42/posts` does not match that pattern.

When several enabled rules match, an exact path wins. After that, the wildcard with more literal segments wins, then the longer pattern, then the earlier rule, then the smaller id. The worked example is in [docs/MVP_REQUIREMENTS.md](docs/MVP_REQUIREMENTS.md).

An empty body is the right shape for `204`. Invalid JSON is rejected and the rules file is left unchanged.

## Fresh request

Check **Fresh request** on a rule when you want the Ruby response instead of the saved mock. The rule still matches the method and path. The proxy then forwards the call and does not use the saved status or body.

The Ruby server must be reachable. This is pass-through of fresh data, not a mock. The response does not include `X-Mock-Response`.

The proxy removes these validators before the forward:

- `If-None-Match`
- `If-Modified-Since`
- `If-Match`
- `If-Unmodified-Since`
- `If-Range`

It sets `Cache-Control: no-cache` and `Pragma: no-cache`. Method, query, body, and other headers stay. The proxy does not turn a `304` into a `200`. If Ruby still answers `304`, the app receives `304`.

Example rule: method `POST`, pattern `/v1/items`, Fresh request checked. The saved `500` body is kept for later, but this call does not return it.

```sh
curl -i -X POST 'http://127.0.0.1:8080/v1/items?page=2' \
  -H 'Content-Type: application/json' \
  -H 'If-None-Match: "old"' \
  -H 'If-Modified-Since: Tue, 15 Nov 1994 08:12:31 GMT' \
  --data '{"n":1}'
```

The rules list shows `fresh` or `mock`. Clear the checkbox to return to the saved mock. Enable, disable, edit, and restart keep the choice.

## Passthrough

With no matching rule, the proxy returns the Ruby response.

```sh
curl -i 'http://127.0.0.1:8080/v1/health?ping=1'
```

`/_admin` is never forwarded. A request to an unknown `/_admin/...` path returns `404` from this process.

## History

Open [http://localhost:8080/_admin/history](http://localhost:8080/_admin/history).

The list shows method, URL, status, source, and local time. The URL is the path plus the query string. Sources are `mock`, `upstream`, `upstream-error`, and `rejected`.

Open a row to see headers and bodies. A preview longer than 64 KiB is marked truncated. The full body is still what the client and the Ruby server exchanged, up to the 8 MiB buffer limit.

Delete one row from the list or the detail page. **Clear all history** opens a confirm page first. Both actions keep the rules. The pages say that rules are kept.

## Tests

```sh
gofmt -w $(go list -f '{{.Dir}}' ./...)
go vet ./...
go test ./...
```

`gofmt` above formats every package directory. You can also format the files you edited.

The tests use `httptest`. They do not need a Ruby server or a Flutter app.

## Manual checklist

Use this on a Mac after the automated tests pass. The implementation environment did not run a macOS GUI session or a Flutter app.

1. `go run ./cmd/mockproxy` listens on loopback and logs the dashboard URL.
2. `LISTEN_ADDR=0.0.0.0:8080` fails before the process listens.
3. With no rules, the Flutter debug client at `http://localhost:8080` receives the Ruby status, body, and query string.
4. The Ruby project and the production base URL are unchanged.
5. The dashboard can create, edit, enable, disable, and delete a rule.
6. An exact rule matches one method and one path, and ignores the query string.
7. A wildcard rule matches one `*` per whole segment and rejects a pattern such as `/api/users/**`.
8. Overlapping rules follow the precedence in the requirements.
9. A mock can return `200`, a `4xx`, and a `5xx`, with headers and a JSON body.
10. A mocked response includes `X-Mock-Response: true`. A passthrough response does not add it.
11. The history list shows method, URL, status, source, and timestamp.
12. A history detail page shows headers and bodies.
13. Delete-one and clear-all remove only history. Clear-all asks for confirmation.
14. After a restart, rules are still there and history is empty.
15. A request under `/_admin` never reaches the Ruby server.
16. `go test ./...` passes.
17. A rule with Fresh request checked returns a full Ruby response with the cache validators removed. A rule without it still returns the saved mock.

## Troubleshooting

| What you see | What to check |
| --- | --- |
| Startup says `UPSTREAM_BASE_URL is required` | Export the Ruby base URL in the same shell. |
| Startup says `LISTEN_ADDR must be a loopback address` | Use `127.0.0.1`, `localhost`, or `[::1]`. |
| Startup says the rules file contains invalid JSON | Fix or remove `data/rules.json`. Startup will not replace a broken file. |
| The app gets a connection error | The proxy is running, and the debug base URL is `http://localhost:8080`. On an Android emulator use `http://10.0.2.2:8080`. |
| A trailing slash hits the wrong Ruby route | `/v1/users` and `/v1/users/` are different paths. |
| `/_admin` shows a Ruby page | The request did not reach this proxy. |
| A mock does not match | The method and path are exact, the rule is enabled, and the pattern is not under `/_admin`. |
| Two rules seem to compete | Read the precedence section in the requirements. Disabled rules never win. |
| Rules vanished after restart | The save failed, or the process was started from a different working directory. |
| History vanished after restart | That is the MVP. History is in memory. Rules stay in the JSON file. |
| Sentry still shows a mock error | `beforeSend` is not dropping `X-Mock-Response: true`. |

## Limits

| Limit | Result |
| --- | --- |
| Mock JSON body over 1 MiB | The dashboard refuses the rule. |
| Request body over 8 MiB | `413`. The request is not forwarded. |
| Upstream response over 8 MiB | `502` with `{"error":"upstream response too large"}`. |
| Upstream timeout or connection failure | `502` with `{"error":"upstream unavailable"}`. |
| History preview | First 64 KiB, with a truncated flag. |
| History rows | `HISTORY_LIMIT`, then the oldest row is dropped. |

These limits protect one local process.

## Outside this MVP

Remote deployment, login, record and replay, added latency, OpenAPI alerts, regular expressions, query-string rules, durable history, and a LAN bind for a physical phone are future work. They are listed in [docs/TASKS.md](docs/TASKS.md).
