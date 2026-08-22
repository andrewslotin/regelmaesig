Regelmäsig
==========

_Make VBB regelmäsig again, or at least its API._

The [VBB API v6](https://v6.vbb.transport.rest/api.html) service has been quite unreliable.
This upsets things like TRMNL that prefer services it regularly polls to be reliable and 
quickly puts them in degraded mode.

Usage
-----

**Docker** (amd64/arm64):

```bash
docker run -p 8080:8080 ghcr.io/andrewslotin/regelmaesig
```

**Go install**:

```bash
go install github.com/andrewslotin/regelmaesig
regelmaesig [-l <listen-addr>] [-t <timeout>]
```

| Flag | Env | Default | Description |
|---|---|---|---|
| `-l` | `VBB_LISTEN_ADDR` | `:8080` | Listen address |
| `-t` | `VBB_TIMEOUT` | `10s` | Upstream request timeout |

The proxy forwards all requests to `https://v6.vbb.transport.rest`. When the upstream returns a non-2xx response, times out, or is unreachable, it returns HTTP 200 with a properly-typed empty JSON body so polling clients stay healthy.

### HAFAS fallback

When transport.rest is down, the proxy can fall back to the underlying HAFAS `mgate.exe` API directly. This is disabled by default and activated via environment variables:

| Env | Required | Default | Description |
|---|---|---|---|
| `HAFAS_ENDPOINT` | yes | — | HAFAS mgate.exe URL (e.g. `https://fahrinfo.vbb.de/bin/mgate.exe`) |
| `HAFAS_AUTH_AID` | yes | — | HAFAS authentication AID (e.g. `hafas-vbb-webapp`) |
| `HAFAS_VERSION` | no | `1.45` | HAFAS protocol version |

Both `HAFAS_ENDPOINT` and `HAFAS_AUTH_AID` must be set to enable the fallback. When enabled, the proxy tries transport.rest first and falls back to HAFAS for: departures, arrivals, locations, nearby, stops, and compact departures.

```bash
HAFAS_ENDPOINT=https://fahrinfo.vbb.de/bin/mgate.exe \
HAFAS_AUTH_AID=hafas-vbb-webapp \
regelmaesig
```

HAFAS responses are translated to the same JSON format as transport.rest, so clients see no difference. Per-upstream metrics are available via the `upstream` label (`"transport_rest"` or `"hafas"`) on `upstream_requests_total`, `upstream_request_duration_seconds`, and `upstream_errors_total`.

Compact endpoint
----------------

Every route above is a 1-to-1 mirror of `v6.vbb.transport.rest`. The `/compact/`
namespace is the exception: it serves a **compact, transformed** multi-stop
departure board for lightweight clients (watch apps, widgets, e-ink displays),
so the client does zero interpretation and payloads stay small (a 2-stop,
6-departure board is well under 2 KB).

```
GET /compact/departures?stops=<id>[,<id>...]&duration=<min>&limit=<n>
```

| Param | Required | Default | Constraints | Meaning |
|---|---|---|---|---|
| `stops` | yes | — | 1–4 comma-separated VBB stop IDs, each 1–12 digits | boards are returned in request order |
| `duration` | no | `30` | clamped to 10–120 | minutes-ahead window |
| `limit` | no | `6` | clamped to 1–10 | max departures per stop |

Only `stops` can produce a `400`; `duration`/`limit` silently fall back to the
default when unparseable and are clamped when out of range. Every product is
included except express (IC/ICE).

### Response format

All responses are `application/json`. Keys are deliberately short, times are
UNIX epoch **seconds**, and delay is in whole **minutes** — all interpretation
is done server-side.

```json
{
  "asOf": 1753440000,
  "partial": 0,
  "boards": [
    {
      "id": "900100003",
      "n": "S+U Alexanderplatz",
      "d": [
        { "l": "U2", "p": "subway", "dir": "Pankow",
          "t": 1753440300, "dl": 2, "c": 0, "w": 0 }
      ]
    }
  ]
}
```

| Field | Type | Meaning |
|---|---|---|
| `asOf` | int | epoch seconds of the freshest upstream realtime update across boards; server time if none reported |
| `partial` | 0/1 | `1` when at least one stop fetch failed (the failed board is omitted, not empty) |
| `boards[].id` | string | stop ID, exactly as requested |
| `boards[].n` | string | stop name (falls back to the stop ID if upstream reports none) |
| `boards[].d` | array | departures, **always an array, never `null`**; empty board → `[]` |
| `d[].l` | string | line name (`"?"` when unknown) |
| `d[].p` | string | product (`subway`, `suburban`, `tram`, `bus`, `ferry`, `regional`; `""` when absent) |
| `d[].dir` | string | destination, with an `S+U `/`U `/`S ` prefix stripped, truncated to 24 characters |
| `d[].t` | int | departure time, epoch seconds (realtime `when`, else `plannedWhen`) |
| `d[].dl` | int | delay in whole minutes (`0` when no realtime data; may be negative for early) |
| `d[].c` | 0/1 | `1` if cancelled (kept in the list, timed by its planned departure) |
| `d[].w` | 0/1 | `1` if the departure carries a warning remark |

Departures are sorted by `t` ascending; those already in the past are dropped,
then `limit` is applied.

### Status codes

| Status | Body | When |
|---|---|---|
| `200` | schema above | at least one stop succeeded, or all failed but a cached board is still valid (served with `X-Cache: HIT`) |
| `400` | `{"error":"bad_request"}` | invalid `stops` parameter |
| `502` | `{"error":"upstream"}` | all stop fetches failed and nothing cached remains |

On upstream failure the endpoint serves the last known good response until its
final departure has passed. `GET /healthz` returns `200 ok`.
