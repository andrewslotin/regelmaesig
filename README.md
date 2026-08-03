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

Watch endpoint
--------------

`GET /garmin/departures?stops=<id>[,<id>...]&duration=<min>&limit=<n>` serves a
compact multi-stop departure board for a Garmin watch app (1–4 stop IDs;
`duration` defaults to 30, `limit` to 6 departures per stop). On upstream
failure it serves the last known good response until its final departure has
passed, and `502 {"error":"upstream"}` only when nothing cached remains.
`GET /healthz` returns `200 ok`.
