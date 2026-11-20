# Automation API

The automation API controls the mock proxy from a script or an AI agent. It listens on the admin port, `http://127.0.0.1:8081` by default. The proxy port does not serve it.

Every route, field, and status code is in [`openapi.yaml`](../openapi.yaml) at the repo root. Paste that file into [editor.swagger.io](https://editor.swagger.io) or import it into Postman when you want a browsable view. This page is the short version you can follow with curl.

## Get the token

The process writes a token to `data/admin.token` on first start. Later starts reuse the file. The log prints the path and never the token.

```sh
TOKEN=$(cat data/admin.token)
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8081/_api/health
```

Do not echo `$TOKEN`, and do not paste it into chat, tickets, or commits. `POST /_api/token/rotate` replaces the file. The old value stops working immediately. Read the file again after a rotate.

Send the header on every call, including health.

```
Authorization: Bearer $TOKEN
```

## Safety

Prefer a test session. It has its own port, rules, and history, and it disappears when you delete it. Nothing it holds is written under `data/`.

The `default` session is the developer proxy on port 8080. Writes to it need the query `confirm=default`. Without that exact value the response is 409 and nothing changes. Reads do not need the flag. Test sessions do not need it.

`DELETE /_api/sessions/default` is always 409. The developer session stays up.

`DELETE /_api/sessions/default/history` does **not** ask for confirmation. It clears recorded traffic. Do not call it unless that is what you want.

The Host must be `localhost`, `127.0.0.1`, or `[::1]` on the admin port. Any `Origin` header is 403. `OPTIONS` is 403. Do not send a browser origin.

JSON bodies use `Content-Type: application/json`. A body over 1 MiB is 413. An unknown JSON field is 400. Scenario load also accepts YAML. See below.

This API cannot create or delete upstreams. A rule's `upstream` is `Any` or the name of an upstream that already exists.

## Errors

Failures share one shape.

```json
{"error":{"code":"unauthorized","message":"a bearer token is required"}}
```

| Status | code | When |
| --- | --- | --- |
| 400 | bad_request | Bad JSON, an unknown field, or a value the route rejects. |
| 401 | unauthorized | The token is missing or wrong. |
| 403 | forbidden | Host is not loopback on the admin port, Origin is set, or the method is OPTIONS. |
| 404 | not_found | Unknown path, session, or rule. |
| 409 | conflict | Default write without `confirm=default`, busy port, duplicate rule, or deleting `default`. |
| 413 | payload_too_large | Body larger than 1 MiB. |
| 415 | unsupported_media_type | Content type is not JSON, or not JSON or YAML on scenario load. |
| 429 | too_many | Eleven test sessions, or 501 rules in one session. |

## Recipes

Set the token once in the shell you are using.

```sh
TOKEN=$(cat data/admin.token)
```

### Create a test session

`port` 0 picks a free port. The response has `id`, `proxy_url`, and `emulator_url`.

```sh
curl -s -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"login-tests","port":0,"idle_timeout_seconds":1800}' \
  http://127.0.0.1:8081/_api/sessions
```

Point a debug build at `proxy_url` on this machine, or at `emulator_url` from an Android emulator. A quiet session closes after `idle_timeout_seconds`. Omitted, that timeout is 30 minutes. Ten test sessions is the cap.

```sh
SESSION=4f2a9c1b8e0d77a1
```

Use the `id` from the response. The value above is only a dummy.

### Mock a 401 on token refresh

This matches `POST /v1/oauth/token` when the body contains `grant_type=refresh_token`. The body in the rule is dummy data.

```sh
curl -s -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"refresh-fail","description":"Refresh token is rejected","upstream":"Any","method":"POST","path":"/v1/oauth/token","body_conditions":["grant_type=refresh_token"],"status":401,"headers":{"Content-Type":"application/json"},"body":{"error":"invalid_grant","error_description":"dummy refresh failure"}}' \
  "http://127.0.0.1:8081/_api/sessions/$SESSION/rules"
```

`description` is the label the dashboard shows as Name. `name` is the stable slug. Other condition forms are in the spec. A few you will use are `page=2`, `q:contains=hotel`, `token:present`, and `debug:absent`.

Call the session proxy, not the admin port.

```sh
curl -s -i -X POST \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  -d 'grant_type=refresh_token&refresh_token=dummy' \
  "http://127.0.0.1:PORT/v1/oauth/token"
```

Replace `PORT` with the session's `port`.

### Wait for traffic and poll history

History is newest first. `since` is the last entry id you stored. The next call returns only newer rows. `limit` defaults to 100 and cannot exceed 500. `Authorization` and `Cookie` are masked unless you add `reveal=1`.

```sh
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8081/_api/sessions/$SESSION/history?method=POST&path_exact=/v1/oauth/token&mocked=true&limit=100"
```

Take `entries[0].id` once you have a row, then poll.

```sh
SINCE=c8d4e5f678901234
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8081/_api/sessions/$SESSION/history?since=$SINCE&limit=100"
```

An unknown `since` is 400. An empty `entries` array means nothing new arrived. Wait, then call it again.

Other filters are `path` (substring), `upstream`, `status`, `rule_name`, and `rule_id`. They combine.

### Load a YAML scenario

`mode=merge` updates rules that share a name and leaves the rest. `mode=replace` is the default and drops rules that are not in the file. A bad file changes nothing.

```sh
curl -s -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/yaml' \
  --data-binary @scenarios/okya-auth-refresh-failure.yaml \
  "http://127.0.0.1:8081/_api/sessions/$SESSION/scenarios/load?mode=merge"
```

The response lists `created`, `updated`, and `unchanged` names. `application/json`, `text/yaml`, and `application/x-yaml` are accepted too. The API does not take a filesystem path except as the body you send.

### Export rules

```sh
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8081/_api/sessions/$SESSION/scenario?format=yaml"
```

Leave off `format`, or set anything other than `yaml`, and the body is JSON.

### Reset

An empty body clears rules and history. Set a field to false to keep that side. Resetting `default` needs `confirm=default`.

```sh
curl -s -o /dev/null -w '%{http_code}\n' -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"rules":true,"history":true}' \
  "http://127.0.0.1:8081/_api/sessions/$SESSION/reset"
```

204 means it cleared.

### Close the session

```sh
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8081/_api/sessions/$SESSION"
```

204 means the test proxy is gone. Rules and history for that session are gone with it.

## For AI agents

Follow this order unless the user asked for something narrower.

1. Read [`openapi.yaml`](../openapi.yaml) before you invent a field. The Go handlers are the source of truth, and the spec is checked against them in `internal/api/openapi_test.go`.
2. Read the token with `TOKEN=$(cat data/admin.token)`. Do not print it. Do not call `POST /_api/token/rotate` unless the user asked.
3. Check `GET /_api/health` with the bearer token. Host must be loopback on port 8081. Do not send `Origin`.
4. Create a test session with `POST /_api/sessions`. Keep the `id`, `proxy_url`, and `port`.
5. Change rules on that session. Prefer `PUT /_api/sessions/{id}/rules/by-name/{name}` so a second run replaces the same slug instead of inserting a duplicate.
6. Send traffic to `proxy_url`. Poll `GET /_api/sessions/{id}/history?since={lastId}`. Store the newest `id`. An empty list means you should wait and poll again.
7. Load `scenarios/*.yaml` with `POST .../scenarios/load?mode=merge` when the user already has a scenario file. Do not point the API at a path on disk and expect it to open the file.
8. Delete the test session when the task is done.
9. Touch `default` only when the user asked to change the developer proxy. Add `?confirm=default` to every write. That includes create, replace, delete, enable, disable, named upsert, scenario load, and reset. `GET` does not need it.
10. Do not call `DELETE .../history` on `default` as a cleanup step. It does not require `confirm=default`.

`description` on a rule is the human label. `name` is the slug `[a-z0-9][a-z0-9._-]{0,63}`. `upstream` is `Any` or an existing upstream name. There is no `/_api` route for upstream records.

A replace (`PUT`) sends the whole rule. An omitted `enabled` means true.
