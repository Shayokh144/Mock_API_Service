# Mock API proxy

A local Go proxy for one Flutter developer on a MacBook.

Point the debug build at `http://localhost:8080`. A matching rule returns a saved response. Every other call goes to your upstream server (develop/staging). That server stays unchanged. Production app configuration stays unchanged.

Behavior, matching, and deletion rules live in [docs/MVP_REQUIREMENTS.md](docs/MVP_REQUIREMENTS.md). The build order is in [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md). The task list is in [docs/TASKS.md](docs/TASKS.md).

## Prerequisites

- macOS
- Go 1.22 or newer. This module was tested with Go 1.22.2.
- Any HTTP or HTTPS backend your Mac can reach
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

The process reads only environment variables. It does not take flags, and it does not store upstream credentials.

| Variable | Required | Default | Meaning |
| --- | --- | --- | --- |
| `UPSTREAM_BASE_URL` | no | none | Absolute `http` or `https` URL. When `upstreams.json` has no upstreams, this seeds the first one and makes it active. A later start does not replace saved upstreams. A path prefix is allowed. A query or fragment is rejected. |
| `LISTEN_ADDR` | no | `127.0.0.1:8080` | Loopback address. `localhost` becomes `127.0.0.1`. `[::1]` is allowed. Any other host fails startup. |
| `RULES_PATH` | no | `data/rules.json` | Rule file. A missing file means there are no rules. |
| `HISTORY_LIMIT` | no | `500` | Newest history entries kept in memory. |
| `UPSTREAM_TIMEOUT` | no | `30s` | Time allowed for one upstream call. |

```sh
export UPSTREAM_BASE_URL='https://upstream.example.com'
```

A path prefix is joined in front of the request path. A trailing slash on the request path stays.

`https://upstream.example.com` plus `GET /v1/users?page=2` becomes `GET https://upstream.example.com/v1/users?page=2`.

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

`data/rules.json` and `data/upstreams.json` are ignored by git. `data/.gitkeep` stays in the repository.

If `UPSTREAM_BASE_URL` is unset and no upstream is saved, the process still listens. The log adds `no upstream configured; add one at the dashboard`.

## Upstreams

Open [http://127.0.0.1:8080/_admin/upstreams](http://127.0.0.1:8080/_admin/upstreams).

1. Choose **New upstream**.
2. Name it, for example `staging`.
3. Set the base URL to an absolute `http` or `https` URL. A path on that URL is kept and joined in front of the forwarded path. A query or fragment is rejected.
4. Optionally set a prefix, one path segment such as `/okya`. It goes on the app base URL, not on the upstream URL.
5. Save. The first upstream is the active one.
6. Add another, then choose **Use**. That upstream is the fallback for requests that match no prefix. Do not restart the proxy.
7. On **New rule** or **Edit**, choose that upstream, or **Any upstream**.

A named rule matches when the request resolves to that upstream, by its prefix or as the fallback. **Any upstream** matches for every resolved upstream, and also when none is resolved. Rules see the path after the prefix is removed. When both match the same path, the more specific pattern still wins. If the patterns tie, the named rule wins over Any.

Passthrough uses the resolved upstream. Its base URL path is joined in front of the forwarded path.

With no matching prefix and no active upstream, passthrough returns `503` and `{"error":"no upstream is configured; add one at /_admin"}`. A matching Any rule still returns the mock.

## Two apps at once

Give each upstream its own prefix when two apps should use the proxy at the same time. The prefix is part of the app base URL. It is not part of the upstream base URL.

1. Create an upstream named `easyHotel`. Set the base URL to the real API, for example `https://staging.example.com`. Set the prefix to `/easyhotel`.
2. Create an upstream named `OKYA`. Set its base URL to that API, and its prefix to `/okya`.
3. Point the easyHotel debug build at `http://127.0.0.1:8080/easyhotel`. On the Android emulator use `http://10.0.2.2:8080/easyhotel`.
4. Point OKYA at `http://127.0.0.1:8080/okya`, or `http://10.0.2.2:8080/okya` on the emulator. The iOS Simulator uses `127.0.0.1`, same as the Mac.

A call to `http://127.0.0.1:8080/okya/api/v1/x?page=2` is forwarded to the OKYA upstream as `/api/v1/x?page=2`. The proxy removes only the prefix. `/okyax` does not match `/okya`. A request to exactly `/okya` is forwarded as `/`.

The Upstreams page shows those two app addresses for each prefixed upstream.

A request with no prefix still goes to the active upstream, the one marked by **Use**. Leave the prefix empty when a single app should keep using `http://127.0.0.1:8080` with no extra segment.

Rules use the path after the prefix. A rule for `/api/v1/x` on OKYA matches `/okya/api/v1/x` and does not match `/easyhotel/api/v1/x`. An Any rule matches both.

You cannot delete the active upstream. Choose **Use** on another one first. Deleting any other upstream sets rules that named it to Any upstream. If that would duplicate an Any rule for the same method and path, the delete is refused and those rules stay unchanged.

Upstreams are stored in `upstreams.json` next to the rules file. The default path is `data/upstreams.json`. Writes are atomic, same as the rules file.

## Connect a Flutter debug build

Change only the debug base URL.

- iOS Simulator and Flutter desktop on the same Mac: `http://localhost:8080`
- Android emulator: `http://10.0.2.2:8080`

Inside the Android emulator, `localhost` is the emulator itself. `10.0.2.2` is the Mac's loopback address. Leave the release host unchanged.

A physical phone cannot use `localhost`. That setup is outside this MVP.

The numbered setup for the Android Emulator and the iOS Simulator is in the next section.

The proxy forwards the method, path, query string, body, and ordinary headers. The upstream server keeps its own auth. Do not put passwords in this repository.

## Using with mobile emulators/simulators

Use these steps for a Flutter debug build on the Android Emulator or the iOS Simulator.

### Shared setup

1. Start the proxy with the real API base:

```sh
UPSTREAM_BASE_URL=https://staging.example.com go run ./cmd/mockproxy
```

The default `LISTEN_ADDR` is `127.0.0.1:8080`. The log prints `dashboard at http://localhost:8080/_admin`.

2. Open [http://127.0.0.1:8080/_admin](http://127.0.0.1:8080/_admin). `localhost` and `127.0.0.1` are the same loopback address here.
3. Choose **New rule** and save a rule.
4. Call that path. A mocked response includes `X-Mock-Response: true`. A passthrough response does not.

Keep any path from the real base URL on the app base URL in the platform steps below. Leave that path off `UPSTREAM_BASE_URL`. The proxy joins the `UPSTREAM_BASE_URL` path in front of the request path. A path on both sides is sent twice.

An upstream prefix, such as `/okya`, is different. Put that prefix on the app base URL too, for example `http://10.0.2.2:8080/okya`. Leave it off the upstream base URL. The proxy removes it before forwarding. The Upstreams page shows the address to copy.

Example: the app used `https://staging.example.com/v1`. The Android base URL is `http://10.0.2.2:8080/v1`. The iOS Simulator base URL is `http://127.0.0.1:8080/v1`. Start with `UPSTREAM_BASE_URL=https://staging.example.com`.

The app keeps calling the proxy. To move between staging and develop, add both on the Upstreams page and choose **Use**. `UPSTREAM_BASE_URL` only seeds the first upstream when `upstreams.json` is empty. It does not switch the active one on a later start.

### Android Emulator

1. Point the debug base URL at `http://10.0.2.2:8080`, plus any path prefix from the real base URL.
2. `10.0.2.2` is the emulator's alias for the host loopback. Inside the emulator, `localhost` is the emulator itself.
3. A loopback-only `LISTEN_ADDR` still works. The default is `127.0.0.1:8080`. The emulator maps `10.0.2.2` to that host loopback. A LAN bind is not required.
4. Allow cleartext HTTP in the debug build only. Add `android/app/src/debug/res/xml/network_security_config.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<network-security-config>
    <domain-config cleartextTrafficPermitted="true">
        <domain includeSubdomains="false">10.0.2.2</domain>
    </domain-config>
</network-security-config>
```

Reference it from the debug manifest, `android/app/src/debug/AndroidManifest.xml`, on `<application>`:

```xml
android:networkSecurityConfig="@xml/network_security_config"
```

`android:usesCleartextTraffic="true"` on that same debug `<application>` also allows cleartext. The domain config limits cleartext to `10.0.2.2`. Do not add either setting to the release manifest.

5. Pass the base URL with `--dart-define` or a flavor. Leave the release host in source unchanged.

```sh
flutter run --dart-define=API_BASE_URL=http://10.0.2.2:8080
```

The app reads that value, for example with `String.fromEnvironment('API_BASE_URL')`. The proxy does not read that name.

### iOS Simulator

The Simulator shares the Mac's network. Use `http://127.0.0.1:8080` or `http://localhost:8080`, plus any path prefix from the real base URL.

App Transport Security blocks plain HTTP until local networking is allowed. In `ios/Runner/Info.plist`, add:

```xml
<key>NSAppTransportSecurity</key>
<dict>
	<key>NSAllowsLocalNetworking</key>
	<true/>
</dict>
```

Keep that change debug-only, or check with the team before committing it. If `NSAppTransportSecurity` is already present with an empty dict, add the inner key there.

Run it with `flutter run -d <simulator>`, or run the same Simulator from Xcode.

A physical device is out of scope for this loopback-only MVP.

### If the app does not show traffic

- Cleartext or ATS errors: the debug network security config must allow `10.0.2.2`, or `NSAllowsLocalNetworking` must be true.
- The app still calls the old base URL: the debug define or flavor did not win. This run must be a debug build.
- Connection refused or a timeout: the proxy is not running, or the app port is not the proxy port. The default port is `8080`.
- The call works and history stays empty: the app is not reaching this proxy. Open [http://127.0.0.1:8080/_admin/history](http://127.0.0.1:8080/_admin/history) and check the base URL host.

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

Check **Fresh request** on a rule when you want the upstream response instead of the saved mock. The rule still matches the method and path. The proxy then forwards the call and does not use the saved status or body.

The upstream server must be reachable. This is pass-through of fresh data, not a mock. The response does not include `X-Mock-Response`.

The proxy removes these validators before the forward:

- `If-None-Match`
- `If-Modified-Since`
- `If-Match`
- `If-Unmodified-Since`
- `If-Range`

It sets `Cache-Control: no-cache` and `Pragma: no-cache`. Method, query, body, and other headers stay. The proxy does not turn a `304` into a `200`. If the upstream server still answers `304`, the app receives `304`.

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

With no matching rule, the proxy returns the upstream response.

```sh
curl -i 'http://127.0.0.1:8080/v1/health?ping=1'
```

`/_admin` is never forwarded. A request to an unknown `/_admin/...` path returns `404` from this process.

## History

Open [http://localhost:8080/_admin/history](http://localhost:8080/_admin/history).

The list shows method, URL, status, source, upstream, and local time. The URL is the path plus the query string. Sources are `mock`, `upstream`, `upstream-error`, and `rejected`.

The upstream column is the resolved upstream. A prefix match and the active fallback both store that upstream's id and name, including mocks. The detail page shows the app path and the forwarded path, with the query string on both. The live JSON feed uses the same upstream name. New rows keep the column.

The top of the page filters that list:

| Link | Address |
| --- | --- |
| All | `/_admin/history` |
| Mocked | `/_admin/history?source=mock` |
| Real server | `/_admin/history?source=upstream` |

The page names the active filter and how many rows are shown. A bad `source` value, such as `?source=nope`, shows All. `upstream-error` and `rejected` have no button. They show under All. You can still open `?source=upstream-error` or `?source=rejected` directly.

The upstream menu is a GET form, so it works with JavaScript off. **All upstreams** is the default. **Any upstream (mock rule not linked)** shows older rows stored as `any`. New rows store the resolved upstream, so a mock from an Any rule appears under that upstream. Each saved upstream is listed by its current name. The query is `upstream=<id>`, and it combines with `source`. A value that is not `any` or a 16-digit hex id is ignored, same as a bad `source`. The live feed uses that same query.

Each row stores the upstream id and the name from the moment of the request. Renaming an upstream does not change old rows, and the filter still matches them by id. Deleting an upstream leaves those rows in history. Open `?upstream=<id>` to see them. The menu labels that id with the stored name and `(deleted)` while you are on that filter. Rows with no upstream, such as a request made before any upstream existed, stay under **All upstreams**.

While the list is open, it asks for new rows about once a second and adds them at the top. The active filter still applies. Rows already on the page stay put, and an open detail page is left alone. Reload the list if JavaScript is off.

A fresh-request rule is a real server response, not a mock. Its source stays `upstream`, so **Real server** includes it. The source column says `upstream (fresh rule)` when a rule selected that response. A failed fresh request stays `upstream-error` and shows under All.

Open a row to see headers and bodies. A preview longer than 64 KiB is marked truncated. The full body is still what the client and the upstream server exchanged, up to the 8 MiB buffer limit.

History stores a decoded copy of the body when `Content-Encoding` is `gzip`, `deflate` (zlib or raw), `br`, or `identity`. Stacked encodings are undone from the outside in. The bytes and headers sent to the client stay unchanged. The decoded copy stops at 64 KiB. A corrupt or unknown encoding is a short note, not compressed bytes. A binary or non-UTF-8 body is a note with the size and content type. JSON is indented. Request bodies follow the same rules. Decoding happens when the row is recorded, so history keeps the preview text.

Delete one row from the list or the detail page. The list returns to the same filter. **Clear all history** opens a confirm page first. Clear removes every entry, including rows the filter is hiding. Both actions keep the rules. The pages say that rules are kept.

## Tests

```sh
gofmt -w $(go list -f '{{.Dir}}' ./...)
go vet ./...
go test ./...
```

`gofmt` above formats every package directory. You can also format the files you edited.

The tests use `httptest`. They do not need a running upstream server or a Flutter app.

## Manual checklist

Use this on a Mac after the automated tests pass. The implementation environment did not run a macOS GUI session or a Flutter app.

1. `go run ./cmd/mockproxy` listens on loopback and logs the dashboard URL.
2. `LISTEN_ADDR=0.0.0.0:8080` fails before the process listens.
3. With no rules, the Flutter debug client at `http://localhost:8080` receives the upstream status, body, and query string.
4. The upstream project and the production base URL are unchanged.
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
15. A request under `/_admin` never reaches the upstream server.
16. `go test ./...` passes.
17. A rule with Fresh request checked returns a full upstream response with the cache validators removed. A rule without it still returns the saved mock.
18. History can show All, Mocked, or Real server. A fresh-request row appears under Real server as `upstream (fresh rule)`. A bad source query shows All. Delete keeps the filter. Clear all still removes every entry.
19. The Upstreams page can add two base URLs, and **Use** switches passthrough without a restart. A rule for one upstream does not match while the other is active. An Any rule still matches. History shows the upstream name. An old rules file still loads.
20. Two prefixed upstreams can serve at once. `/okya/api/v1/x?page=2` is forwarded as `/api/v1/x?page=2`, and `/okyax` does not match `/okya`. A request with no prefix uses the active upstream. History keeps the original path and the forwarded path. An old upstreams file with no prefix field still loads.

## Troubleshooting

| What you see | What to check |
| --- | --- |
| Startup says `UPSTREAM_BASE_URL is required` | Export the upstream base URL in the same shell. |
| Startup says `LISTEN_ADDR must be a loopback address` | Use `127.0.0.1`, `localhost`, or `[::1]`. |
| Startup says the rules file contains invalid JSON | Fix or remove `data/rules.json`. Startup will not replace a broken file. |
| The app gets a connection error | The proxy is running, and the debug base URL is `http://localhost:8080`. On an Android emulator use `http://10.0.2.2:8080`. |
| A trailing slash hits the wrong upstream route | `/v1/users` and `/v1/users/` are different paths. |
| `/_admin` shows an upstream page | The request did not reach this proxy. |
| A mock does not match | The method and path are exact, the rule is enabled, and the pattern is not under `/_admin`. |
| Two rules seem to compete | Read the precedence section in the requirements. Disabled rules never win. |
| Rules vanished after restart | The save failed, or the process was started from a different working directory. |
| History vanished after restart | That is the MVP. History is in memory. Rules stay in the JSON file. |
| Sentry still shows a mock error | `beforeSend` is not dropping `X-Mock-Response: true`. |
| Passthrough returns `no upstream is configured` | Add an upstream at `/_admin/upstreams`, or set `UPSTREAM_BASE_URL` before the first start. |
| A mock stopped matching after a switch | The rule names an upstream that is not active. Choose **Use**, or set the rule to Any upstream. |

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
