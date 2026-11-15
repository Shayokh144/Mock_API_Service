# Implementation Plan

This plan builds the proxy described in [MVP_REQUIREMENTS.md](MVP_REQUIREMENTS.md). Follow that file when a behavior detail is in question.

The reader is new to Go. Each phase has a learning goal, the files to add, ordered steps, a check, and a done condition. Stay on the phase until its check passes.

Use the standard library. The imports you need are `net/http`, `net/http/httputil`, `net/http/httptest`, `net/url`, `encoding/json`, `html/template`, `embed`, `sync`, `os`, `io`, `bytes`, `crypto/rand`, `time`, and `path/filepath`.

## Working slice

Phases 1 and 2 make a passthrough proxy. Phase 3 is the first full slice: one fixed mock, and every other path still reaches upstream. You can demonstrate that slice with `curl` before any dashboard exists.

Later phases replace the fixed mock with saved rules, files, wildcards, history, and HTML.

## Layout

Create the module at the repository root. The module path is `github.com/Shayokh144/Mock_API_Service`.

```text
cmd/mockproxy/main.go
internal/config/config.go
internal/proxy/handler.go
internal/proxy/handler_test.go
internal/rules/rule.go
internal/rules/match.go
internal/rules/match_test.go
internal/rules/store.go
internal/rules/store_test.go
internal/history/log.go
internal/history/log_test.go
internal/admin/server.go
internal/admin/server_test.go
internal/admin/templates/rules.html
internal/admin/templates/rule_form.html
internal/admin/templates/history.html
internal/admin/templates/history_detail.html
internal/admin/templates/history_clear.html
data/.gitkeep
```

`internal/` means only this module can import those packages. `cmd/mockproxy` is the program you run.

`data/.gitkeep` keeps the directory in git. The real `data/rules.json` stays untracked. That file is created on the first successful save.

## Phase 1 — A process that listens

**Learn:** a Go module, `func main`, and `http.Server`.

**Files:** `go.mod`, `cmd/mockproxy/main.go`, `internal/config/config.go`.

1. Run `go mod init github.com/Shayokh144/Mock_API_Service`.
2. In `main`, load config, build the handler, and call `ListenAndServe`.
3. Log `listening on 127.0.0.1:8080` and `dashboard at http://localhost:8081/_admin` after the address is known. The dashboard listens on `127.0.0.1:8081`.
4. Read configuration only from the environment. Use the names and defaults in the requirements.
5. Parse `UPSTREAM_BASE_URL` with `url.Parse`. Require `http` or `https`, and require a host. Reject a query or a fragment.
6. Resolve `LISTEN_ADDR` and allow only loopback. `localhost` becomes `127.0.0.1`. `[::1]` stays as written.
7. Parse `UPSTREAM_TIMEOUT` with `time.ParseDuration`. Parse `HISTORY_LIMIT` as a positive integer.
8. On any bad setting, print the error to stderr and exit with status 1. Do not listen.

**Check:**

```sh
UPSTREAM_BASE_URL=http://127.0.0.1:9 go run ./cmd/mockproxy
```

The log shows the listen address and the dashboard URL. The process keeps running. Stop it with Ctrl+C. `LISTEN_ADDR=0.0.0.0:8080` exits before listening.

**Done when:** startup accepts a valid loopback config and rejects a non-loopback host.

## Phase 2 — Passthrough

**Learn:** `httputil.ReverseProxy`, URL joining, and one handler for every API path.

**Files:** `internal/proxy/handler.go`.

1. Keep a single HTTP handler. Avoid `ServeMux` for API paths. The default mux redirects some trailing slashes, and those redirects would hide upstream routes. Match and forward `r.URL.Path` as parsed. Do not call `path.Clean`.
2. If the path is `/_admin` or has the prefix `/_admin/`, return `404` and `the dashboard is not served on the proxy port`. Do not call upstream. The dashboard is the separate admin listener.
3. Build a `ReverseProxy`. In `Director`, copy the incoming request onto the upstream URL.
4. Join paths without dropping a trailing slash. If the base path is empty or `/`, use the request path. Otherwise trim one trailing slash from the base path and concatenate the request path.
5. Copy `RawQuery` unchanged. Set `req.Host` and `req.URL.Host` to the upstream host. Set `req.URL.Scheme` from the base URL.
6. Give the proxy a timeout from `UPSTREAM_TIMEOUT`. On failure, write `502` and `{"error":"upstream unavailable"}`.
7. Leave upstream status, headers, and body unchanged on success.

**Check:** start a tiny upstream with a few lines of Go or with any local HTTP server. Point `UPSTREAM_BASE_URL` at it.

```sh
curl -i 'http://127.0.0.1:8080/v1/users?page=2'
curl -i 'http://127.0.0.1:8080/_admin'
curl -i 'http://127.0.0.1:8081/_admin'
```

The first command returns the upstream body and the query. The second returns `404` and the upstream server sees no `/_admin` request.

**Done when:** an unmatched path returns the upstream response, and `/_admin` is never forwarded.

## Phase 3 — Vertical slice

**Learn:** handle the mock before the proxy, and write headers before the body.

**Files:** `internal/proxy/handler.go`, `internal/proxy/handler_test.go`.

This phase is the end-to-end slice. Keep it small on purpose.

1. Before passthrough, match `GET` and path `/__slice/ping`.
2. Write `Content-Type: application/json`, then `X-Mock-Response: true`, then status `200`, then the body `{"ok":true}`.
3. Call `Header().Set` before `WriteHeader`. `Write` sends the status if you have not sent one yet.
4. Any other method or path still uses Phase 2 passthrough.
5. Add `httptest` coverage:
   - A fake upstream from `httptest.NewServer`.
   - `GET /__slice/ping` returns `200`, the JSON body, and `X-Mock-Response: true`. The fake upstream sees zero requests.
   - `GET /v1/ping?x=1` returns the fake upstream status and body, with the query intact, and without `X-Mock-Response`.
   - `GET /_admin` returns `404` and the fake upstream sees zero requests.

**Check:** `go test ./internal/proxy`.

**Done when:** those three tests pass. Leave the fixed path in place until Phase 4 deletes it.

## Phase 4 — Exact rules in memory

**Learn:** structs, validation, and `sync.RWMutex`.

**Files:** `internal/rules/rule.go`, `internal/rules/match.go`, `internal/rules/store.go`.

1. Define `Rule` with the fields in the requirements. Store `Body` as `json.RawMessage`. Store an empty body as `null`.
2. Validate method, status, name length, headers, body size, and path pattern. Exact patterns are the only patterns accepted in this phase. Reject any pattern that contains `*`.
3. Reject a pattern of `/_admin` or a pattern with prefix `/_admin/`.
4. Normalize the method with `strings.ToUpper`.
5. Implement exact match as string equality of the path. Ignore `RawQuery`.
6. Put rules in a store guarded by `sync.RWMutex`. Create, update, delete, get, and list methods take the lock.
7. Assign `id` from 8 cryptographically random bytes, hex encoded. Set `created_at` and `updated_at` in UTC.
8. Reject a create or update whose method and pattern already belong to another id.
9. Copy the winning rule out of the lock before writing the HTTP response. Do not hold the lock during a client or upstream write.
10. Replace the `/__slice/ping` branch with a store lookup. Delete that temporary path.

Match only enabled rules. A miss falls through to passthrough.

**Check:** unit tests for equal paths, different query strings, disabled rules, and a duplicate method plus pattern. A handler test creates an exact rule, requests that path with a query, and sees the mock.

**Done when:** an enabled exact rule beats passthrough, and the temporary slice path is gone.

## Phase 5 — Save rules as JSON

**Learn:** `encoding/json`, and replace a file by renaming in the same directory.

**Files:** `internal/rules/store.go`.

1. Use this on-disk shape:

```json
{
  "rules": []
}
```

2. If `RULES_PATH` is missing, start with an empty list. Create parent directories on the first save.
3. If the file exists and JSON decoding fails, return the error from startup. Leave the file untouched.
4. To save, encode into `rules.json.tmp` in the same directory, close the file, then `os.Rename` it onto the target.
5. Set the file mode to `0600`.
6. Update the in-memory slice only after rename returns nil.
7. On a rename error, keep the old memory and return the error to the caller.

**Check:** create a rule, stop the process, start it, and see the same rule match. Change the file into `{` and see startup fail. Confirm the broken file is still `{`.

**Done when:** a restart keeps rules, and a broken file cannot be overwritten by a failed start.

## Phase 6 — Wildcards and precedence

**Learn:** splitting strings, and a total ordering for ties.

**Files:** `internal/rules/match.go`, `internal/rules/match_test.go`.

1. Split a pattern on `/`. A usable pattern starts with `/`.
2. Allow `/` as an exact pattern. Reject every other empty segment.
3. A segment is either literal text or the single character `*`. Reject `**`, `?`, and a star glued to text.
4. A wildcard matches when the segment counts are equal, literal segments are equal, and each `*` covers one segment.
5. Apply the precedence order from the requirements: exact, then more literal segments, then longer pattern text, then earlier `created_at`, then smaller `id`.
6. Keep the unique method-plus-pattern check from Phase 4.
7. Add table tests for the A, B, and C example in the requirements. Add a case where `created_at` breaks a tie. Add rejected pattern strings.

**Check:** `go test ./internal/rules`.

**Done when:** the example requests select A and then B, and invalid patterns never enter the store.

## Phase 7 — Status, headers, and JSON

**Learn:** `http.ResponseWriter` order, and canonical header names.

**Files:** `internal/proxy/handler.go`, `internal/rules/rule.go`.

1. Validate `status_code` from 100 through 599.
2. Parse the submitted body as JSON when it is non-empty. Store the compact raw JSON. Cap the stored body at 1 MiB.
3. When writing a mock, copy rule headers except hop-by-hop names and `X-Mock-Response`.
4. If the body is non-empty and `Content-Type` is missing, set `application/json`.
5. Set `X-Mock-Response` to `true` after the rule headers.
6. Call `WriteHeader`, then write the body. An empty body writes nothing, which is what `204` needs.
7. Test `201`, `404`, and `500`. Test a custom header. Test that a rule header `X-Mock-Response: false` still leaves the response as `true`.
8. Test that header map order does not matter. Assert each header, not a full header block string.

**Check:** `go test ./internal/proxy ./internal/rules`.

**Done when:** a saved `4xx` or `5xx` rule returns that status, the JSON body, and `X-Mock-Response: true`.

## Phase 8 — History

**Learn:** buffering a body so it can be both stored and forwarded.

**Files:** `internal/history/log.go`, changes in `internal/proxy/handler.go`.

1. Define the history fields from the requirements.
2. Guard the log with a mutex. `Add`, `List`, `Get`, `Delete`, and `Clear` are the only operations.
3. `List` returns newest first.
4. Before matching, read the request body up to 8 MiB plus one byte. If it is larger, respond `413` with `{"error":"request body too large"}`, record source `rejected`, and return.
5. Put the full buffered bytes back on `req.Body` with `io.NopCloser`. The upstream must see the full body.
6. Store only the first 64 KiB in the history entry. Set `request_body_truncated` when the body is longer.
7. For a mock, record source `mock`, the rule id, and the response you wrote.
8. For passthrough, use `ReverseProxy.ModifyResponse` to buffer an upstream body up to 8 MiB. Replace `resp.Body` with the buffered bytes so the client still receives them. Record source `upstream`.
9. If the upstream body exceeds 8 MiB, or the upstream call fails, write the `502` JSON from the requirements and record source `upstream-error`.
10. After a successful add, drop the oldest entries until the count is within `HISTORY_LIMIT`.
11. `Delete` removes one id and leaves the others. `Clear` removes every entry. Neither function opens the rules file.

**Check:** tests for mock, upstream, upstream error, `413`, truncation flags, the cap, delete-one, and clear-all. Restart is covered by using a new log value and seeing zero entries.

**Done when:** every handled request appends one entry, and deletion never changes rules.

## Phase 9 — Dashboard

**Learn:** `html/template`, `embed`, and POST then redirect.

**Files:** `internal/admin/server.go` and the templates listed above.

1. Add `//go:embed templates/*.html` and parse the templates at startup. A parse error stops startup.
2. Route with a small path switch under `/_admin`. This switch is separate from API matching.
3. Escape all rule and history text through `html/template`. Do not use `template.HTML` on user input.
4. Implement the routes in the requirements table. A missing id returns `404`.
5. Render the rules list and the history list with the columns named in the requirements.
6. The form fields are name, enabled, method, path pattern, status code, headers, and body. Edit one header per line as `Name: value`.
7. On a validation error, render the form again with status `400` and the submitted values. Do not redirect away from the error.
8. On success, save through the store, then respond `303` to the list.
9. The clear-history GET page explains that rules are kept. Its POST calls `Clear`.
10. Show timestamps in `time.Local`.
11. Cover the flows with `httptest`: create, edit, disable, delete, history detail, delete one entry, and clear all. Assert the rules file after create and after a history delete.

**Check:** `go test ./internal/admin`. Then run the process and click through the same flows in a browser.

**Done when:** a rule created in the browser still matches after a restart, and clearing history leaves that rule in place.

## Phase 10 — README and the manual pass

**Learn:** how to leave a program another person can run.

**Files:** `README.md`, `.gitignore`.

1. Replace the one-line README with run instructions: Go version (current stable), working directory, environment variables, `go run ./cmd/mockproxy`, the dashboard URL, and `go test ./...`.
2. Document the debug base URL and the `X-Mock-Response` contract. Point to `docs/MVP_REQUIREMENTS.md` for matching and deletion.
3. Add a manual checklist that copies the acceptance checklist from the requirements.
4. Ignore `data/rules.json` and any built binary. Keep `data/.gitkeep`.

**Check:** from a clean shell, follow the README only. Run the tests. Create a `500` rule in the browser. Call it with `curl -i`. Restart and call it again. Clear history and confirm the rule still matches.

**Done when:** a new terminal session can complete that check using the README and no other notes.

## Fresh request

This behavior is task MVP-38. It sits on top of saved rules and passthrough.

1. Add `fresh_request` to the rule and the dashboard form. Default it to false so older files keep returning mocks.
2. When the winning rule has the flag, skip `writeMock`. Strip `If-None-Match`, `If-Modified-Since`, `If-Match`, `If-Unmodified-Since`, and `If-Range`. Set `Cache-Control: no-cache` and `Pragma: no-cache`. Then use the existing forward path.
3. Leave method, query, body, and other headers in place. Do not rewrite a `304` into a `200`.
4. Show fresh or mock on the rules list. Keep the flag across edit, enable, disable, and restart.
5. Test with `httptest`: validators removed and a full upstream body returned, the flag off still mocks, a disabled rule does not strip headers, and the JSON file round-trips the flag.

**Done when:** a checked Fresh request rule returns the upstream body, and an unchecked rule still returns the saved mock.

## History source filter

This behavior is task MVP-39. It sits on the history list.

1. Add All, Mocked, and Real server links on `GET /_admin/history`. Mocked uses `?source=mock`. Real server uses `?source=upstream`.
2. Parse the query with the history source values. An unknown value shows every entry and does not echo the bad value in later links.
3. Print the active filter and the number of entries shown. Keep the query on row, delete, detail, and clear links.
4. Leave fresh-request rows as source `upstream` with a rule id. Show `upstream (fresh rule)` in the source column. Do not add a new source value.
5. Delete redirects back to the same filter. Clear all still deletes every entry, then returns to that filter.
6. Keep the existing Content-Security-Policy. Use links, not a script.

**Done when:** Mocked hides real responses, Real server shows passthrough and fresh-request rows, and a bad `source` query shows All.

## Troubleshooting

| What you see | What to check |
| --- | --- |
| The client gets an empty reply | `WriteHeader` was called after `Write`, or the status was sent twice |
| A trailing slash hits the wrong upstream route | Path joining trimmed the slash |
| `/_admin` shows an upstream page | The admin branch runs after the proxy |
| Tests hang | An `httptest` server was left without `Close` |
| Rules vanish after restart | Memory was updated, and the rename failed or never ran |
| Two rules seem random | The winner was not sorted by the full precedence list |
| A test fails only sometimes | It compared header block order, or map iteration order |

## Definition of done

The implementation is finished when the acceptance checklist in [MVP_REQUIREMENTS.md](MVP_REQUIREMENTS.md) is true and every MVP task in [TASKS.md](TASKS.md) is checked. Future tasks stay undone.
