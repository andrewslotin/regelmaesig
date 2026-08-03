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
