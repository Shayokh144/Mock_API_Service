# Tasks

Tickets for the local mock API proxy. Behavior lives in [MVP_REQUIREMENTS.md](MVP_REQUIREMENTS.md). The build order lives in [IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md).

Do the MVP tasks in id order unless a dependency says otherwise. A task is ready when every id in **Depends on** is done. Future tasks are not part of the MVP.

Each task is one change you can review on its own. Tests belong with the behavior they lock in.

## MVP

### Phase 1 — Process and config

#### MVP-01 — Start the Go module

- **Outcome:** `cmd/mockproxy` is a program the module can build.
- **Depends on:** none
- **Acceptance criteria:**
  - `go.mod` uses module path `github.com/Shayokh144/Mock_API_Service`.
  - `go run ./cmd/mockproxy` compiles. With config still missing, it may exit. It does not need a network listen yet if MVP-02 lands in the same change.
  - No third-party dependencies are added.

#### MVP-02 — Load local configuration

- **Outcome:** Startup reads the environment and refuses a non-local bind.
- **Depends on:** MVP-01
- **Acceptance criteria:**
  - The variables, defaults, and validation match the requirements table.
  - A missing or invalid `UPSTREAM_BASE_URL` exits with status 1 and names the setting.
  - `LISTEN_ADDR=0.0.0.0:8080` exits with status 1 before `ListenAndServe`.
  - A valid config logs the listen address and `http://localhost:8080/_admin`.

### Phase 2 — Passthrough

#### MVP-03 — Forward unmatched requests

- **Outcome:** A request that is not reserved is sent to Ruby, and the client receives the real response.
- **Depends on:** MVP-02
- **Acceptance criteria:**
  - `httputil.ReverseProxy` performs the forward.
  - Method, path, trailing slash, query, and body reach the upstream URL described in the requirements.
  - The outbound `Host` is the upstream host.
  - Upstream status, headers, and body are returned to the client.

#### MVP-04 — Keep `/_admin` off the Ruby server

- **Outcome:** The reserved prefix is handled only by this process.
- **Depends on:** MVP-03
- **Acceptance criteria:**
  - `/_admin` and `/_admin/anything` return a proxy `404`.
  - The upstream test server records zero requests for those paths.
  - No other prefix is reserved.

#### MVP-05 — Test passthrough

- **Outcome:** The forward and the reserved prefix stay locked by tests.
- **Depends on:** MVP-03, MVP-04
- **Acceptance criteria:**
  - `httptest` is the upstream. No real Ruby server is required.
  - A query string and a trailing slash are asserted.
  - `go test ./internal/proxy` passes.

### Phase 3 — Vertical slice

#### MVP-06 — Serve one fixed mock

- **Outcome:** `GET /__slice/ping` returns a mock, and every other path still passes through.
- **Depends on:** MVP-03
- **Acceptance criteria:**
  - The status is `200` and the body is `{"ok":true}`.
  - The fake upstream receives no request for that path.
  - `POST /__slice/ping` still passes through.

#### MVP-07 — Mark the fixed mock for Sentry

- **Outcome:** The slice response carries the filter header.
- **Depends on:** MVP-06
- **Acceptance criteria:**
  - The slice response has `X-Mock-Response: true`.
  - A passthrough response from this phase does not add that header.

#### MVP-08 — Test the vertical slice

- **Outcome:** One test run shows both the mock and a real passthrough.
- **Depends on:** MVP-05, MVP-07
- **Acceptance criteria:**
  - The three cases from Phase 3 of the plan are covered: slice mock, passthrough with query, and `/_admin`.
  - `go test ./internal/proxy` passes.

### Phase 4 — Exact rules in memory

#### MVP-09 — Define and validate a rule

- **Outcome:** A `Rule` value can be accepted or rejected before it is stored.
- **Depends on:** MVP-01
- **Acceptance criteria:**
  - Fields match the requirements, including `json.RawMessage` for the body.
  - Allowed methods and status range are enforced.
  - Patterns under `/_admin` are rejected.
  - This task may still reject every pattern that contains `*`.

#### MVP-10 — Match an exact path

- **Outcome:** Enabled exact rules match method plus path, and ignore the query.
- **Depends on:** MVP-09
- **Acceptance criteria:**
  - Path equality is case-sensitive.
  - `?page=1` and `?page=2` hit the same rule.
  - A disabled rule does not match.
  - A different method does not match.

#### MVP-11 — Store rules in memory

- **Outcome:** The process can list, create, update, get, and delete rules safely.
- **Depends on:** MVP-09
- **Acceptance criteria:**
  - A `sync.RWMutex` guards the collection.
  - Ids are 16 hex characters from `crypto/rand`.
  - A duplicate method and pattern is rejected, excluding the rule being updated.
  - Timestamps are UTC.

#### MVP-12 — Use the store on the request path

- **Outcome:** Saved exact rules replace the fixed slice mock.
- **Depends on:** MVP-06, MVP-10, MVP-11
- **Acceptance criteria:**
  - The handler copies the winning rule and then releases the lock before writing.
  - A miss uses passthrough.
  - `/__slice/ping` is no longer special.

#### MVP-13 — Test exact matching

- **Outcome:** Exact match behavior is covered without the dashboard.
- **Depends on:** MVP-10, MVP-12
- **Acceptance criteria:**
  - Tests cover a hit, a query-string hit, a disabled rule, a method miss, and a duplicate save.
  - `go test ./internal/rules ./internal/proxy` passes.

### Phase 5 — Rule file

#### MVP-14 — Write rules atomically

- **Outcome:** Each successful change replaces `RULES_PATH` safely.
- **Depends on:** MVP-11
- **Acceptance criteria:**
  - The JSON shape matches the sample in the requirements.
  - The write uses a temp file in the same directory and `os.Rename`.
  - The file mode is `0600`.
  - Memory changes only after a successful rename.

#### MVP-15 — Load rules on startup

- **Outcome:** Restart restores rules, and a broken file stops startup.
- **Depends on:** MVP-14
- **Acceptance criteria:**
  - A missing file starts as an empty rule list.
  - Invalid JSON returns an error and does not rewrite the file.
  - A loaded exact rule matches on the first request after start.

#### MVP-16 — Test persistence

- **Outcome:** File behavior is covered in `go test`.
- **Depends on:** MVP-15
- **Acceptance criteria:**
  - Tests use a temp directory, not the developer's real `data/rules.json`.
  - They cover a round trip, a missing file, and invalid JSON.
  - `go test ./internal/rules` passes.

### Phase 6 — Wildcards

#### MVP-17 — Match `*` segments

- **Outcome:** Wildcard patterns follow the segment rules in the requirements.
- **Depends on:** MVP-10
- **Acceptance criteria:**
  - `*` matches one whole segment.
  - Segment count must be equal.
  - Glued stars, `**`, missing leading `/`, and empty segments are rejected.
  - The pattern is not compiled as a regular expression.

#### MVP-18 — Choose one winner

- **Outcome:** Overlapping enabled rules use the documented precedence.
- **Depends on:** MVP-17, MVP-11
- **Acceptance criteria:**
  - The sort is exact, literal-segment count, pattern length, `created_at`, then `id`.
  - The requirements example selects rule A for `/api/users/1` and rule B for `/api/users/2`.
  - Duplicate method-plus-pattern saves are still rejected.

#### MVP-19 — Test wildcards and precedence

- **Outcome:** The matching table is locked in tests.
- **Depends on:** MVP-18
- **Acceptance criteria:**
  - Tests include the A/B/C example, a `created_at` tie, and each rejected pattern class.
  - `go test ./internal/rules` passes.

### Phase 7 — Mock body and headers

#### MVP-20 — Return the saved response

- **Outcome:** A winning rule writes its status, headers, and JSON body.
- **Depends on:** MVP-12, MVP-09
- **Acceptance criteria:**
  - `200`, `4xx`, and `5xx` can all be returned.
  - Empty body writes no bytes.
  - Bodies over 1 MiB and invalid JSON are refused on save.
  - A missing `Content-Type` on a non-empty body becomes `application/json`.

#### MVP-21 — Force the Sentry header

- **Outcome:** Mocked responses always expose `X-Mock-Response: true`.
- **Depends on:** MVP-20
- **Acceptance criteria:**
  - The header is set after rule headers.
  - A rule value of `false` still produces `true` on the wire.
  - Passthrough and proxy errors do not add the header.

#### MVP-22 — Test mock responses

- **Outcome:** Status, headers, and the forced header are covered.
- **Depends on:** MVP-21
- **Acceptance criteria:**
  - Tests request `201`, `404`, and `500`.
  - Tests assert headers one by one.
  - `go test ./internal/proxy ./internal/rules` passes.

### Phase 8 — History

#### MVP-23 — Record each exchange

- **Outcome:** Every finished request appends one in-memory history entry.
- **Depends on:** MVP-03, MVP-12
- **Acceptance criteria:**
  - Sources are `mock`, `upstream`, `upstream-error`, and `rejected`.
  - Entries include method, path, query, status, headers, bodies, timestamp, and rule id.
  - The forwarded body is the full buffered body, not only the preview.

#### MVP-24 — Cap the log

- **Outcome:** The log keeps at most `HISTORY_LIMIT` entries.
- **Depends on:** MVP-23
- **Acceptance criteria:**
  - The oldest entry is dropped first.
  - Body previews stop at 64 KiB and set the truncated flags.
  - A request over 8 MiB returns `413`, source `rejected`, and is not forwarded.
  - An upstream failure or oversized upstream body returns the documented `502`.

#### MVP-25 — Delete history

- **Outcome:** One entry or the whole log can be removed without touching rules.
- **Depends on:** MVP-23
- **Acceptance criteria:**
  - Delete removes only the named id.
  - Clear removes every entry.
  - The rules file bytes are unchanged by both operations.
  - A new store value starts empty, which is the restart behavior.

#### MVP-26 — Test history

- **Outcome:** Recording, caps, and deletion have tests.
- **Depends on:** MVP-24, MVP-25
- **Acceptance criteria:**
  - Tests cover all four sources, the cap, truncation, delete-one, and clear-all.
  - `go test ./internal/history ./internal/proxy` passes.

### Phase 9 — Dashboard

#### MVP-27 — Render the rules list

- **Outcome:** A browser can open `/_admin` and see the current rules.
- **Depends on:** MVP-11
- **Acceptance criteria:**
  - Templates are embedded with `embed`.
  - The list shows name, method, pattern, kind, enabled state, and status code.
  - Rule text is HTML-escaped.

#### MVP-28 — Create a rule in the browser

- **Outcome:** The new-rule form adds a rule and redirects.
- **Depends on:** MVP-14, MVP-27
- **Acceptance criteria:**
  - `POST /_admin/rules` uses the same validation as the store.
  - Success responds `303` to the rules list.
  - A bad body returns `400` on the form and does not change the file.

#### MVP-29 — Edit a rule in the browser

- **Outcome:** An existing rule can be changed from the dashboard.
- **Depends on:** MVP-28
- **Acceptance criteria:**
  - The edit form shows the current values.
  - Update keeps the same id and `created_at`.
  - Update refreshes `updated_at` and the file.

#### MVP-30 — Enable, disable, and delete a rule

- **Outcome:** The three remaining rule actions work from the list.
- **Depends on:** MVP-28
- **Acceptance criteria:**
  - Toggle flips `enabled` and saves.
  - Delete removes that rule from memory and from the file.
  - A missing id returns `404`.

#### MVP-31 — Render the history list

- **Outcome:** `/_admin/history` shows the columns from the requirements.
- **Depends on:** MVP-23, MVP-27
- **Acceptance criteria:**
  - Columns are method, URL, status, source, and timestamp.
  - The URL is path plus query.
  - The newest entry is first.
  - The timestamp uses the local zone.

#### MVP-32 — Inspect one history entry

- **Outcome:** Opening an entry shows headers and bodies.
- **Depends on:** MVP-31
- **Acceptance criteria:**
  - Request and response headers and bodies are visible.
  - Truncation is visible when a flag is set.
  - An unknown id returns `404`.

#### MVP-33 — Delete history from the dashboard

- **Outcome:** The dashboard performs delete-one and confirmed clear-all.
- **Depends on:** MVP-25, MVP-31
- **Acceptance criteria:**
  - Delete-one is a POST for that id.
  - Clear-all requires the confirm page, then POST.
  - Both pages state that rules remain.
  - After clear-all, the rules list is unchanged.

#### MVP-34 — Test the dashboard

- **Outcome:** The HTML flows are covered without a browser driver.
- **Depends on:** MVP-30, MVP-32, MVP-33
- **Acceptance criteria:**
  - `httptest` covers create, validation failure, edit, toggle, delete rule, detail, delete one entry, and clear-all.
  - A history delete test asserts the rules file is unchanged.
  - `go test ./internal/admin` passes.

### Phase 10 — README and ignore rules

#### MVP-35 — Write the run instructions

- **Outcome:** `README.md` is enough to start the proxy and the tests.
- **Depends on:** MVP-02, MVP-34
- **Acceptance criteria:**
  - It states the working directory, the environment variables, `go run ./cmd/mockproxy`, the dashboard URL, and `go test ./...`.
  - It states the debug base URL `http://localhost:8080` and the Android emulator exception `http://10.0.2.2:8080`.
  - It states the `X-Mock-Response: true` contract for Sentry `beforeSend`.
  - It links to `docs/MVP_REQUIREMENTS.md` for matching, precedence, and deletion.

#### MVP-36 — Record the manual acceptance pass

- **Outcome:** The README contains the manual checklist from the requirements.
- **Depends on:** MVP-35
- **Acceptance criteria:**
  - The checklist includes passthrough, a `4xx` or `5xx` mock, the Sentry header, history detail, delete-one, clear-all, and restart.
  - Restart is described as rules kept and history empty.
  - The Ruby project and the production base URL are called out as unchanged.

#### MVP-37 — Ignore local rule data

- **Outcome:** Saved rules and binaries do not become commits.
- **Depends on:** MVP-14
- **Acceptance criteria:**
  - `.gitignore` includes `data/rules.json` and the built binary name.
  - `data/.gitkeep` remains so the directory exists.
  - `git status` after a local save shows no rule-file change.

### Fresh request

#### MVP-38 — Bypass cache validators for one rule

- **Outcome:** A rule can forward a fresh upstream request instead of returning its saved mock.
- **Depends on:** MVP-12, MVP-14, MVP-30
- **Acceptance criteria:**
  - `fresh_request` defaults to false. Existing mock rules keep returning the saved status, headers, body, and `X-Mock-Response: true`.
  - When the winning enabled rule has `fresh_request` set, the proxy forwards upstream and does not write the saved mock.
  - That forward removes `If-None-Match`, `If-Modified-Since`, `If-Match`, `If-Unmodified-Since`, and `If-Range`, then sets `Cache-Control: no-cache` and `Pragma: no-cache`.
  - Method, query, body, and other headers stay. A `304` is not rewritten into a `200`.
  - The dashboard checkbox edits the flag. The rules list shows fresh or mock. Enable, disable, and restart keep the flag.
  - The Ruby server must be reachable. The mode is pass-through of fresh data, not a mock response.
  - `httptest` covers validators removed, the upstream body returned, the flag off, persistence, and the dashboard.

### History filter

#### MVP-39 — Filter history by source

- **Outcome:** The history page can show all entries, mocked entries, or real server entries.
- **Depends on:** MVP-31, MVP-33, MVP-38
- **Acceptance criteria:**
  - `GET /_admin/history` offers All, Mocked (`?source=mock`), and Real server (`?source=upstream`).
  - The page names the active filter and the number of entries shown. Refresh keeps the query.
  - An unknown `source` value shows All and is not copied into later links.
  - A fresh-request success stays source `upstream` with a rule id. Real server includes it. The source column says `upstream (fresh rule)`.
  - `upstream-error` and `rejected` stay under All. They are not mocks.
  - Delete returns to the active filter. Clear all still removes every entry, including hidden ones, and does not change rules.
  - The page has no script. The existing Content-Security-Policy stays.
  - Tests cover filter selection, query parsing, an invalid value, the active filter text, and delete and clear.

## Future

These tasks are out of the MVP. They have no MVP dependency that forces them now. Do them only after the acceptance checklist is already true.

#### FUT-01 — Persist history

- **Outcome:** History can survive a restart.
- **Depends on:** MVP-26
- **Acceptance criteria:**
  - The storage choice and the delete-one and clear-all semantics are updated in the requirements before code changes.
  - A restart test shows the chosen new behavior.

#### FUT-02 — Match on query strings

- **Outcome:** Two rules can differ by query string.
- **Depends on:** MVP-19
- **Acceptance criteria:**
  - The requirements define the query syntax and precedence before code changes.
  - Existing path-only rules keep their current matches.

#### FUT-03 — Record and replay

- **Outcome:** A passthrough exchange can become a rule from the history detail page.
- **Depends on:** MVP-32
- **Acceptance criteria:**
  - The created rule is visible in the rules file.
  - Replay stays a separate action from normal passthrough.

#### FUT-04 — Inject latency

- **Outcome:** A rule can delay its mock response.
- **Depends on:** MVP-21
- **Acceptance criteria:**
  - The delay is configured per rule.
  - Tests use a short delay and a timeout, not a real-time sleep of many seconds.

#### FUT-05 — Protect the dashboard

- **Outcome:** `/_admin` requires a local secret.
- **Depends on:** MVP-34
- **Acceptance criteria:**
  - API passthrough stays available without that secret.
  - The requirements describe how the secret is supplied on one MacBook.

#### FUT-06 — Separate users

- **Outcome:** Rules and history are scoped to more than one user.
- **Depends on:** FUT-05
- **Acceptance criteria:**
  - The requirements define the user boundary before code changes.
  - One user's delete does not remove another user's rules.

#### FUT-07 — Deploy remotely

- **Outcome:** The proxy can run somewhere other than the developer MacBook.
- **Depends on:** MVP-36
- **Acceptance criteria:**
  - The requirements replace the loopback-only startup rule on purpose.
  - Production Flutter configuration stays out of that change until a separate decision says otherwise.

#### FUT-08 — OpenAPI and contract alerts

- **Outcome:** A spec can seed rules, and drift can be reported.
- **Depends on:** MVP-18
- **Acceptance criteria:**
  - Import creates normal rules in `rules.json`.
  - Alerts are defined in the requirements before any notifier is added.

#### FUT-09 — Regular-expression patterns

- **Outcome:** A rule may use a regex path when a wildcard is not enough.
- **Depends on:** MVP-19
- **Acceptance criteria:**
  - Regex is a third pattern kind. Existing exact and wildcard rules stay valid.
  - Precedence against wildcards is written into the requirements first.

#### FUT-10 — Reach a physical device

- **Outcome:** A phone on the same network can use the proxy.
- **Depends on:** MVP-02
- **Acceptance criteria:**
  - The listen-address rule is widened only for this task.
  - The README describes the LAN URL as a debug base URL, not a production host.
