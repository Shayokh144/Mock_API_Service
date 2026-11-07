# MVP Requirements

The product is a local mock API proxy for one Flutter developer on a MacBook.

The Flutter debug build calls `http://localhost:8080`. This Go service answers that address. A matched rule returns a saved response. Every other call goes to your upstream server (develop/staging). That server stays unchanged. Production app configuration stays unchanged.

Read this file for behavior. [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md) says how to build it. [TASKS.md](TASKS.md) splits the work into tickets.

## Glossary

- **Proxy**: the Go process on the MacBook.
- **Upstream**: any HTTP or HTTPS backend your Mac can reach.
- **Passthrough**: a request the proxy forwards upstream.
- **Rule**: one saved mock.
- **Exact pattern**: a path with no `*`.
- **Wildcard pattern**: a path that uses `*` as a whole segment.
- **Dashboard**: the pages under `/_admin`.
- **History entry**: one recorded request and response.

## MVP behavior

The proxy runs on the developer's MacBook. It is not deployed.

The Flutter app changes only its debug base URL to `http://localhost:8080`. Release builds keep the real API host.

The proxy forwards an unmatched request to the configured upstream server. It returns that server's status, headers, and body.

The dashboard can create, edit, enable, disable, and delete rules.

A rule matches an HTTP method plus an exact path, or a method plus a wildcard path.

A rule returns a chosen status code, headers, and JSON body. Status codes include `4xx` and `5xx`.

Every mocked response includes `X-Mock-Response: true`. Flutter Sentry `beforeSend` can drop those events.

The dashboard lists history with method, URL, status, source, and timestamp. Opening an entry shows headers and bodies.

The developer can delete one history entry or clear all history.

Rules remain after a restart. History does not.

The upstream server needs no code, route, or config change. Production app configuration needs no change.

## Out of this MVP

Later work, tracked in [TASKS.md](TASKS.md), covers:

- Remote deployment
- Accounts, sessions, and dashboard login
- Recording traffic and replaying it
- Added latency
- OpenAPI import and contract alerts
- Regular-expression patterns
- Rules that differ by query string
- History that survives a restart
- Binding a non-loopback address for a physical phone

## Local setup

Supported debug clients are the iOS Simulator and Flutter desktop on the same Mac. Both reach `127.0.0.1` through `localhost`.

The Android emulator is different. Inside that emulator, `localhost` is the emulator. Use `http://10.0.2.2:8080` as the debug base URL for an emulator run. Leave the release host alone.

A physical phone cannot use `localhost`. Reaching a phone over the LAN is later work.

Run the proxy from a terminal on the Mac. Stop it with Ctrl+C. The upstream server must already be reachable from the Mac.

### Listen address

The process listens on `127.0.0.1:8080` by default. Startup accepts only a loopback address: `127.0.0.1`, `localhost`, or `[::1]`. Any other host fails startup with an error.

### Environment

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `UPSTREAM_BASE_URL` | yes | none | Absolute `http` or `https` URL of your upstream server (develop/staging) |
| `LISTEN_ADDR` | no | `127.0.0.1:8080` | Loopback address and port |
| `RULES_PATH` | no | `data/rules.json` | Rule file, relative to the working directory |
| `HISTORY_LIMIT` | no | `500` | Newest history entries kept in memory |
| `UPSTREAM_TIMEOUT` | no | `30s` | Time allowed for one upstream call |

`UPSTREAM_BASE_URL` must include a scheme and host. A path prefix is allowed. A query string or fragment is rejected at startup.

Example:

```sh
export UPSTREAM_BASE_URL='https://upstream.example.com'
go run ./cmd/mockproxy
```

Open the dashboard at `http://localhost:8080/_admin`.

### Path joining

The incoming path and query are appended to the upstream base.

- Base `https://upstream.example.com` and request `/v1/users?page=2` become `https://upstream.example.com/v1/users?page=2`.
- Base `https://upstream.example.com/prefix` and request `/v1/users` become `https://upstream.example.com/prefix/v1/users`.
- A trailing slash on the request path stays.

The proxy sets the outbound `Host` header to the upstream host. Hop-by-hop headers follow `httputil.ReverseProxy`.

### Flutter and Sentry

Two debug-only edits live in the Flutter app. This repository does not contain the Flutter app.

1. Point the debug base URL at `http://localhost:8080`.
2. In Sentry `beforeSend`, drop an event when the HTTP response header `X-Mock-Response` equals `true`. Header lookup is case-insensitive.

Passthrough responses do not get that header from the proxy. Real upstream failures still reach Sentry.

The Dart types that expose response headers depend on the app's Sentry SDK version. Use the header contract above with the types that SDK version provides.

### Limits

| Limit | Value | Result |
| --- | --- | --- |
| Mock JSON body | 1 MiB | The dashboard refuses a larger body |
| Request body read by the proxy | 8 MiB | A larger request returns `413` and is not forwarded |
| Upstream response buffered | 8 MiB | A larger response becomes a proxy `502` |
| History body preview | 64 KiB | The extra bytes are marked truncated and still forwarded |
| History rows | `HISTORY_LIMIT` | The oldest row is dropped |

These limits keep one local process safe. They are not a public traffic policy.

## Request handling

Every connection is handled in this order:

1. If the path is `/_admin` or starts with `/_admin/`, the dashboard handles it. The proxy does not match rules and does not call upstream.
2. The proxy records the start time and buffers the request body, up to the limit above.
3. It selects the winning enabled rule for the method and path.
4. A winning rule writes the mock response.
5. Otherwise the proxy calls upstream and returns that response.
6. It stores one history entry.

`/_admin` is the only reserved prefix. Do not add a health route that could hide a real upstream path. Startup logs are the "process is up" signal.

Unknown `/_admin/...` paths return `404` from the proxy. They are not forwarded.

### Proxy-generated errors

| Case | Status | History source | `X-Mock-Response` |
| --- | --- | --- | --- |
| Upstream timeout, connection failure, or bad upstream response | `502` | `upstream-error` | absent |
| Request body over 8 MiB | `413` | `rejected` | absent |
| Upstream response over 8 MiB | `502` | `upstream-error` | absent |

The `502` and `413` bodies are JSON: `{"error":"<short reason>"}`.

A normal upstream response keeps its status and body. Its history source is `upstream`. The proxy does not add `X-Mock-Response`.

## Matching

The match key is the HTTP method plus the full path. The scheme and host are not part of the pattern. Every client request already targets the proxy. The dashboard stores a path such as `/api/v1/profile`, not `http://localhost:8080/api/v1/profile`.

The path is `r.URL.Path` with no extra normalization. A trailing slash stays. A `.` or `..` segment is ordinary text. Do not run `path.Clean` on it, because that removes a trailing slash. The query is `URL.RawQuery`. The fragment is not sent to a server. The admin check uses this same path string.

### Query strings

Query strings do not participate in matching.

`GET /v1/items?page=1` and `GET /v1/items?page=2` use the same rule. The original query is stored in history. On passthrough, the original query is forwarded.

One rule covers every query string for that method and path. Separate mocks per query value are later work.

### Exact pattern

An exact pattern contains no `*`. The request path must equal the pattern character for character. Matching is case-sensitive.

`/v1/users` matches only `/v1/users`. It does not match `/v1/users/`, `/v1/users/1`, or `/v1/Users`.

### Wildcard pattern

`*` is the only special character. It must be a whole path segment. It matches one non-empty segment and does not match `/`.

Valid:

- `/v1/users/*` matches `/v1/users/42` and `/v1/users/ada`
- `/v1/*/profile` matches `/v1/me/profile`
- `/v1/users/*/posts/*` matches `/v1/users/42/posts/7`

Invalid patterns are rejected on save:

- `/v1/users/*/` because of the empty segment
- `/v1/users/id*` because `*` is glued to text
- `/v1/users/**` because `**` is not one `*` segment
- `/v1/users/*/**` for the same reason
- `v1/users` because the pattern does not start with `/`
- A pattern equal to `/_admin` or starting with `/_admin/`

The segment counts must be equal. `/v1/users/*` does not match `/v1/users` or `/v1/users/42/posts`.

`*` matches any characters except `/`, including characters that look like regex. The proxy never compiles a regular expression.

### Methods

Each rule stores one method. The saved value is uppercase. Comparison is case-insensitive.

Allowed methods: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`.

A `GET` rule does not match `HEAD`. The request body is not part of matching.

### Precedence

Only enabled rules are candidates. Among candidates whose method and path match, choose one winner:

1. An exact pattern beats every wildcard pattern.
2. The wildcard with more literal segments wins.
3. The longer pattern string wins.
4. The earlier `created_at` wins.
5. The smaller `id` string wins.

Disabled rules stay stored. They never win.

Example rules:

| Id | Pattern | Kind | Literal segments |
| --- | --- | --- | --- |
| A | `/api/users/1` | exact | n/a |
| B | `/api/users/*` | wildcard | 2 (`api`, `users`) |
| C | `/api/*/*` | wildcard | 1 (`api`) |

`GET /api/users/1` matches A, B, and C. A wins because it is exact.

`GET /api/users/2` matches B and C. B wins because it has more literal segments.

### Duplicate patterns

The pair of method and pattern string is unique. Create and update reject a duplicate of that pair, including a disabled rule. The dashboard shows the error and keeps the previous file.

Different patterns may still overlap. Precedence covers that case.

## Mock response

A rule has:

| Field | Rule |
| --- | --- |
| `id` | 16 hex characters, assigned by the proxy |
| `name` | Display label, up to 80 characters. Not used for matching |
| `enabled` | Boolean |
| `method` | One allowed method |
| `path_pattern` | Exact or wildcard path |
| `status_code` | Integer from 100 through 599 |
| `headers` | Map of header name to one value |
| `body` | A JSON value, or empty |
| `created_at`, `updated_at` | UTC RFC3339 timestamps |

The body typed in the dashboard must be empty or one JSON value. Objects and arrays are the normal case. A JSON string, number, or boolean is valid JSON and is allowed. Invalid JSON is rejected on save.

An empty body writes no response body. That is the right shape for `204`.

### Header handling

The proxy copies the rule headers, then applies these fixes:

- It drops hop-by-hop headers: `Connection`, `Keep-Alive`, `Proxy-Authenticate`, `Proxy-Authorization`, `Te`, `Trailer`, `Transfer-Encoding`, and `Upgrade`.
- It sets `Content-Type` to `application/json` when the body is non-empty and the rule did not set `Content-Type`.
- It sets `X-Mock-Response` to `true` last. A rule cannot turn this header off or change its value.

Header names written to the client use canonical MIME format.

Write the status code after the final headers.

## Dashboard

The dashboard is server-rendered HTML. Forms use POST, then a `303` redirect. Vanilla JavaScript is optional. The MVP does not need a separate JSON API.

| Action | Method and path |
| --- | --- |
| Rules list | `GET /_admin` and `GET /_admin/rules` |
| New-rule form | `GET /_admin/rules/new` |
| Create rule | `POST /_admin/rules` |
| Edit form | `GET /_admin/rules/{id}/edit` |
| Update rule | `POST /_admin/rules/{id}` |
| Enable or disable | `POST /_admin/rules/{id}/toggle` |
| Delete rule | `POST /_admin/rules/{id}/delete` |
| History list | `GET /_admin/history` |
| History detail | `GET /_admin/history/{id}` |
| Delete one history entry | `POST /_admin/history/{id}/delete` |
| Confirm clear | `GET /_admin/history/clear` |
| Clear all history | `POST /_admin/history/clear` |

The rules list shows name, method, pattern, exact or wildcard, enabled state, and status code.

The history list shows method, URL, status, source, and timestamp. The URL is the path plus the query string. The detail page adds request headers, request body, response headers, and response body.

Timestamps are stored in UTC and shown in the Mac's local zone.

Delete-one happens on POST. Clear-all has a confirm page, then POST. Both actions change history only.

A failed validation redisplays the form with the error and the submitted values. It does not write the file.

## Persistence and deletion

### Rules

Rules are stored in `RULES_PATH`. The default file is `data/rules.json` in the working directory. Run the process from the repository root.

The file is a single JSON object:

```json
{
  "rules": [
    {
      "id": "0123456789abcdef",
      "name": "Profile failure",
      "enabled": true,
      "method": "GET",
      "path_pattern": "/api/v1/profile",
      "status_code": 500,
      "headers": {
        "Content-Type": "application/json"
      },
      "body": {
        "error": "planned failure"
      },
      "created_at": "2026-11-05T12:00:00Z",
      "updated_at": "2026-11-05T12:00:00Z"
    }
  ]
}
```

`body` is a JSON value, not a quoted blob. An empty body is `null`.

Save behavior:

- A missing file means there are no rules. The next successful save creates it.
- A file with invalid JSON stops startup. The process must not replace that file with an empty list.
- Each change is written to a temporary file in the same directory, then renamed over the target.
- The in-memory rules update only after the rename succeeds.
- A failed save leaves the previous memory and the previous file in place.
- The file mode is `0600`.

Restart loads the file. Disable and edit survive restart. Delete removes the rule from the file.

### History

History is in memory only. Restart, crash, and Ctrl+C drop it. They do not drop rules.

Deleting one entry removes that id immediately. Clearing all entries removes every entry immediately. Neither action reads or writes the rules file.

When a new entry would exceed `HISTORY_LIMIT`, the oldest entry is removed. The limit counts entries after that drop.

History fields:

| Field | Meaning |
| --- | --- |
| `id` | Unique for this process |
| `started_at` | UTC time when handling began |
| `method`, `path`, `raw_query` | Incoming request line |
| `request_headers`, `request_body` | What the client sent |
| `status` | Status returned to the client |
| `response_headers`, `response_body` | What the client received |
| `source` | `mock`, `upstream`, `upstream-error`, or `rejected` |
| `rule_id` | Winning rule, or empty |
| `request_body_truncated`, `response_body_truncated` | Preview was cut at 64 KiB |

Deleting a rule does not delete history. A later detail page may show a `rule_id` whose rule is already gone.

## Acceptance checklist

The MVP is complete when every line below is true.

1. `go run ./cmd/mockproxy` listens on loopback on a Mac and logs the dashboard URL.
2. A non-loopback `LISTEN_ADDR` fails startup.
3. With no rules, a Flutter debug client using `http://localhost:8080` receives the upstream response, including status, body, and query string.
4. The upstream project and the production base URL are unchanged.
5. The dashboard can create, edit, enable, disable, and delete a rule.
6. An exact rule matches one method and one path, and ignores the query string.
7. A wildcard rule matches one `*` per whole segment and rejects regex-style patterns.
8. Overlapping rules follow the precedence list in this file.
9. A mock can return `200`, a `4xx`, and a `5xx`, with custom headers and a JSON body.
10. A mocked response includes `X-Mock-Response: true`. A passthrough response does not add it.
11. The history list shows method, URL, status, source, and timestamp.
12. A history detail page shows request and response headers and bodies.
13. Delete-one and clear-all remove only history. Clear-all asks for confirmation.
14. After a restart, rules are still there and history is empty.
15. A request under `/_admin` never reaches the upstream server.
16. `go test ./...` passes.
