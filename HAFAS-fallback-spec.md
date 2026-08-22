# Spec: Add HAFAS mgate.exe as fallback data source

## Background

regelmaesig is a reliability proxy for `v6.vbb.transport.rest`. That upstream wraps HAFAS `mgate.exe` via the `hafas-client` JS library. The upstream is volunteer-run and has been down since Aug 20, 2026. The underlying HAFAS API at `fahrinfo.vbb.de/bin/mgate.exe` is still operational.

This change adds HAFAS `mgate.exe` as a fallback data source. When transport.rest fails, the proxy calls HAFAS directly, translates the response into transport.rest-compatible JSON, and serves it. The translated JSON feeds into the existing caching and expiry pipeline unchanged.

HAFAS fallback is **disabled by default**. It activates only when the user provides credentials via environment variables.

## Part 1: DataProvider abstraction and per-upstream metrics (refactor, no behavior change)

### 1.0 Add `upstream` label to metrics

**File: `metrics.go`**

Add an `"upstream"` label to the three upstream metric vectors. This label distinguishes `"transport_rest"` from `"hafas"` so response times and error rates can be tracked separately. `FallbackResponsesTotal` is unchanged — it measures "served from cache or empty JSON after all providers failed," which is upstream-agnostic.

Change the metric registrations:

```go
func NewMetrics(reg *prometheus.Registry) *Metrics {
    factory := promauto.With(reg)
    return &Metrics{
        reg: reg,
        UpstreamRequestsTotal: factory.NewCounterVec(prometheus.CounterOpts{
            Name: "upstream_requests_total",
            Help: "Total requests forwarded to upstream, by upstream, method, path pattern, and HTTP status.",
        }, []string{"upstream", "method", "path", "status"}),
        UpstreamRequestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
            Name:    "upstream_request_duration_seconds",
            Help:    "Upstream response latency in seconds.",
            Buckets: prometheus.DefBuckets,
        }, []string{"upstream", "method", "path"}),
        UpstreamErrorsTotal: factory.NewCounterVec(prometheus.CounterOpts{
            Name: "upstream_errors_total",
            Help: "Upstream failures by upstream, method, path pattern, and reason (timeout, connection_refused, http_5xx, etc.).",
        }, []string{"upstream", "method", "path", "reason"}),
        FallbackResponsesTotal: factory.NewCounterVec(prometheus.CounterOpts{
            Name: "fallback_responses_total",
            Help: "How often the proxy returned an empty fallback instead of upstream data.",
        }, []string{"method", "path"}),
    }
}
```

**Label values**:
- `"transport_rest"` — used by `TransportRESTClient.Forward`, `newPassthroughHandler`, `fetchCompactBoard`, and bespoke handlers (`handleMap`, `handleShape`)
- `"hafas"` — used by `HAFASClient.Departures`, `.Arrivals`, `.Locations`, `.Nearby`, `.Stop`, and `fetchCompactBoardFromHAFAS`

**All existing `.WithLabelValues(r.Method, path, ...)` calls must be updated** to include the upstream label as the first argument: `.WithLabelValues("transport_rest", r.Method, path, ...)`. This applies everywhere metrics are recorded:
- `proxy.go`: `TransportRESTClient.Forward`, `newPassthroughHandler`
- `handle_compact_departures.go`: `fetchCompactBoard`
- `handle_maps.go`: `handleMap`
- `handle_shapes.go`: `handleShape`

This is a label change, not a behavior change — all existing call sites get `"transport_rest"` and continue to work identically. The `"hafas"` value is only used by new code in later parts.

### 1.1 Define `DataProvider` type and `TransportRESTClient`

**File: `proxy.go`**

Add a new function type and struct:

```go
// DataProvider fetches response data for a request.
// A non-nil error means "try next provider."
type DataProvider func(ctx context.Context, r *http.Request, metrics *Metrics) (body []byte, header http.Header, err error)

// TransportRESTClient forwards requests to a transport.rest upstream.
type TransportRESTClient struct {
    Client   *http.Client
    Upstream string
}
```

Add a `Forward` method on `TransportRESTClient` that has the `DataProvider` signature. This method extracts the logic currently inline in `newStandardHandler` (lines 53-76 of the current `proxy.go`): call `forward()`, record metrics, check status, read body, return it.

```go
// Forward implements DataProvider by forwarding the request to the transport.rest upstream.
func (c *TransportRESTClient) Forward(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
    path := routePath(r)

    start := time.Now()
    resp, err := forward(c.Client, c.Upstream, r)
    if err != nil {
        metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", r.Method, path, errorReason(err)).Inc()
        return nil, nil, err
    }
    defer resp.Body.Close()

    duration := time.Since(start)
    metrics.UpstreamRequestDuration.WithLabelValues("transport_rest", r.Method, path).Observe(duration.Seconds())
    metrics.UpstreamRequestsTotal.WithLabelValues("transport_rest", r.Method, path, strconv.Itoa(resp.StatusCode)).Inc()

    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", r.Method, path, httpErrorReason(resp.StatusCode)).Inc()
        return nil, nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, nil, err
    }

    return body, resp.Header.Clone(), nil
}
```

The existing `forward()`, `copyUpstreamResponse()`, `writeEmptyJSON()`, `writeFromCache()`, `serveFallback()`, and `httpErrorReason()` functions remain unchanged.

### 1.2 Refactor `newStandardHandler`

**File: `proxy.go`**

Change the signature of `newStandardHandler` from:

```go
func newStandardHandler(client *http.Client, upstream, emptyBody string, cache *Cache, expiry func([]byte) time.Time, metrics *Metrics) http.HandlerFunc
```

to:

```go
func newStandardHandler(providers []DataProvider, emptyBody string, cache *Cache, expiry func([]byte) time.Time, metrics *Metrics) http.HandlerFunc
```

The body of the handler becomes a loop over providers. On success from any provider, do the existing cache-and-respond logic. If all providers fail, call `serveFallback` (unchanged).

New implementation:

```go
func newStandardHandler(providers []DataProvider, emptyBody string, cache *Cache, expiry func([]byte) time.Time, metrics *Metrics) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        key := r.URL.RequestURI()

        var body []byte
        var header http.Header
        for _, p := range providers {
            var err error
            body, header, err = p(r.Context(), r, metrics)
            if err == nil {
                break
            }
            // Provider failed, try next one.
            body = nil
            header = nil
        }

        if body == nil {
            serveFallback(w, cache, key, emptyBody, metrics, r)
            return
        }

        // Success path (unchanged from current implementation).
        var expiresAt time.Time
        if expiry != nil {
            expiresAt = expiry(body)
        }
        if expiry == nil || (!expiresAt.IsZero() && expiresAt.After(time.Now())) {
            cache.Set(key, &cacheEntry{
                statusCode: http.StatusOK,
                header:     header,
                body:       body,
                expiresAt:  expiresAt,
            })
        }

        for k, vs := range header {
            for _, v := range vs {
                w.Header().Add(k, v)
            }
        }
        w.WriteHeader(http.StatusOK)
        w.Write(body)
    }
}
```

**Important detail**: the current code caches `resp.StatusCode` from the upstream. With the provider abstraction, a successful provider always means the data is good (equivalent to 2xx), so the cached `statusCode` should be `http.StatusOK`. The existing `cacheEntry` struct stores a `statusCode` field — set it to `http.StatusOK` in the provider path.

**Do NOT change** `newPassthroughHandler`'s signature. It stays as-is with `(client *http.Client, upstream, emptyBody string, metrics *Metrics)` since it has no cache and only `/radar` uses it (no HAFAS fallback planned for radar). However, its metric `.WithLabelValues(...)` calls must be updated to include `"transport_rest"` as the first argument (same as Part 1.0).

### 1.3 Refactor all handler functions that use `newStandardHandler`

Every handler that calls `newStandardHandler` must change its signature from `(client *http.Client, upstream string, ...)` to `(providers []DataProvider, ...)`.

Each handler is a thin one-liner. Here is the complete list with the exact change:

**`handle_departures.go`** — change:
```go
// Before:
func handleDepartures(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(client, upstream, `{"departures":[]}`, cache, departuresExpiry, metrics)
}
// After:
func handleDepartures(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{"departures":[]}`, cache, departuresExpiry, metrics)
}
```

**`handle_arrivals.go`** — same pattern:
```go
func handleArrivals(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{"arrivals":[]}`, cache, arrivalsExpiry, metrics)
}
```

**`handle_journeys.go`** — two functions:
```go
func handleJourneys(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{"journeys":[]}`, cache, journeysExpiry, metrics)
}
func handleRefreshJourney(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{"journey":{}}`, cache, refreshJourneyExpiry, metrics)
}
```

**`handle_trips.go`** — two functions:
```go
func handleTrips(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{"trips":[]}`, cache, tripsExpiry, metrics)
}
func handleTrip(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{"trip":{}}`, cache, tripExpiry, metrics)
}
```

**`handle_locations.go`** — two functions:
```go
func handleLocations(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `[]`, cache, nil, metrics)
}
func handleNearby(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `[]`, cache, nil, metrics)
}
```

**`handle_stops.go`** — two functions, but only `handleStop` changes; `handleReachableFrom` also changes signature:
```go
func handleReachableFrom(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{"reachable":[]}`, cache, reachableFromExpiry, metrics)
}
func handleStop(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{}`, cache, nil, metrics)
}
```

**`handle_stations.go`**:
```go
func handleStations(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{}`, cache, nil, metrics)
}
func handleStation(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{}`, cache, nil, metrics)
}
```

**`handle_lines.go`**:
```go
func handleLines(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `[]`, cache, nil, metrics)
}
func handleLine(providers []DataProvider, cache *Cache, metrics *Metrics) http.HandlerFunc {
    return newStandardHandler(providers, `{}`, cache, nil, metrics)
}
```

### 1.4 Handlers with bespoke logic (NOT using `newStandardHandler`)

These handlers have their own inline forward/cache logic. They keep their current `(client *http.Client, upstream string, ...)` signatures unchanged:

- **`handle_maps.go`** (`handleMap`) — custom 2xx+3xx caching, returns 502 on failure
- **`handle_shapes.go`** (`handleShape`) — custom 2xx+3xx caching
- **`handle_radar.go`** (`handleRadar`) — uses `newPassthroughHandler`, no caching
- **`handle_compact_departures.go`** (`handleCompactDepartures`) — custom fan-out logic

These files get no changes in Part 1.

### 1.5 Update `mux.go`

Change `newMux` to create a `TransportRESTClient` and pass provider slices to refactored handlers:

```go
func newMux(upstreamURL string, timeout time.Duration, staticCap, dynamicCap int, metrics *Metrics) *http.ServeMux {
    client := &http.Client{
        Timeout: timeout,
        CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
            return http.ErrUseLastResponse
        },
    }

    rest := &TransportRESTClient{Client: client, Upstream: upstreamURL}
    restOnly := []DataProvider{rest.Forward}

    staticCache := NewCache(staticCap)
    dynamicCache := NewCache(dynamicCap)

    mux := http.NewServeMux()

    mux.Handle("GET /metrics", metrics.Handler())

    mux.HandleFunc("GET /stops/reachable-from", handleReachableFrom(restOnly, dynamicCache, metrics))
    mux.HandleFunc("GET /stops/{id}/departures", handleDepartures(restOnly, dynamicCache, metrics))
    mux.HandleFunc("GET /stops/{id}/arrivals", handleArrivals(restOnly, dynamicCache, metrics))
    mux.HandleFunc("GET /stops/{id}", handleStop(restOnly, staticCache, metrics))

    mux.HandleFunc("GET /journeys/{ref}", handleRefreshJourney(restOnly, dynamicCache, metrics))
    mux.HandleFunc("GET /journeys", handleJourneys(restOnly, dynamicCache, metrics))

    mux.HandleFunc("GET /trips/{id}", handleTrip(restOnly, dynamicCache, metrics))
    mux.HandleFunc("GET /trips", handleTrips(restOnly, dynamicCache, metrics))

    mux.HandleFunc("GET /locations/nearby", handleNearby(restOnly, staticCache, metrics))
    mux.HandleFunc("GET /locations", handleLocations(restOnly, staticCache, metrics))

    // These handlers keep their original (client, upstream, ...) signatures:
    mux.HandleFunc("GET /radar", handleRadar(client, upstreamURL, metrics))
    mux.HandleFunc("GET /stations/{id}", handleStation(restOnly, staticCache, metrics))
    mux.HandleFunc("GET /stations", handleStations(restOnly, staticCache, metrics))
    mux.HandleFunc("GET /lines/{id}", handleLine(restOnly, staticCache, metrics))
    mux.HandleFunc("GET /lines", handleLines(restOnly, staticCache, metrics))
    mux.HandleFunc("GET /shapes/{id}", handleShape(client, upstreamURL, staticCache, metrics))
    mux.HandleFunc("GET /maps/{type}", handleMap(client, upstreamURL, staticCache, metrics))
    mux.HandleFunc("GET /compact/departures", handleCompactDepartures(client, upstreamURL, dynamicCache, metrics))
    mux.HandleFunc("GET /healthz", handleHealthz)

    return mux
}
```

### 1.6 Update test helpers

**File: `testhelpers_test.go`**

The test helpers `newTestStack`, `newUnreachableStack`, and `newCachedTestStack` call `newMux` — their call sites do not change since `newMux`'s external signature does not change in Part 1 (it still accepts the same parameters).

### 1.7 Verification for Part 1

Run `go build ./...` and `go test ./...` — all existing tests must pass. This is a pure refactor with zero behavior change.

---

## Part 2: HAFAS client

### 2.1 New file: `hafas.go`

This file contains the HAFAS mgate.exe client and all supporting types.

#### HAFASClient struct

```go
type HAFASClient struct {
    httpClient *http.Client
    endpoint   string // e.g. "https://fahrinfo.vbb.de/bin/mgate.exe"
    authAID    string // e.g. "hafas-vbb-webapp"
    version    string // e.g. "1.45"
}

func NewHAFASClient(httpClient *http.Client, endpoint, authAID, version string) *HAFASClient {
    if version == "" {
        version = "1.45"
    }
    return &HAFASClient{
        httpClient: httpClient,
        endpoint:   endpoint,
        authAID:    authAID,
        version:    version,
    }
}
```

#### HAFAS request/response protocol

All HAFAS mgate.exe requests are HTTP POST to the endpoint URL with `Content-Type: application/json`. The request body has this structure:

```json
{
    "lang": "en",
    "svcReqL": [{"meth": "<METHOD>", "req": { ... }}],
    "client": {"type": "WEB", "id": "VBB", "name": "VBB WebApp", "l": "vs_webapp_vbb"},
    "ver": "<version>",
    "auth": {"type": "AID", "aid": "<authAID>"}
}
```

The response has this structure:

```json
{
    "ver": "1.45",
    "err": "OK",
    "svcResL": [
        {
            "meth": "<METHOD>",
            "err": "OK",
            "res": { ... }
        }
    ]
}
```

Check both `top-level .err` and `svcResL[0].err` — both must be `"OK"`. If either is not `"OK"`, return an error.

#### Decode structs

Define these structs for decoding HAFAS responses. Only include fields that are actually used — HAFAS responses contain many more fields that should be ignored.

```go
// hafasRequest is the top-level mgate.exe request envelope.
type hafasRequest struct {
    Lang    string            `json:"lang"`
    SvcReqL []hafasSvcRequest `json:"svcReqL"`
    Client  hafasClientID     `json:"client"`
    Ver     string            `json:"ver"`
    Auth    hafasAuth         `json:"auth"`
}

type hafasSvcRequest struct {
    Meth string      `json:"meth"`
    Req  interface{} `json:"req"`
}

type hafasClientID struct {
    Type string `json:"type"`
    ID   string `json:"id"`
    Name string `json:"name"`
    L    string `json:"l"`
}

type hafasAuth struct {
    Type string `json:"type"`
    AID  string `json:"aid"`
}

// hafasResponse is the top-level mgate.exe response envelope.
type hafasResponse struct {
    Err     string           `json:"err"`
    SvcResL []hafasSvcResult `json:"svcResL"`
}

type hafasSvcResult struct {
    Meth string          `json:"meth"`
    Err  string          `json:"err"`
    Res  json.RawMessage `json:"res"`
}

// -- StationBoard response --

type hafasStationBoardResult struct {
    Common   hafasCommon    `json:"common"`
    Type     string         `json:"type"` // "DEP" or "ARR"
    JnyL     []hafasJourney `json:"jnyL"`
    PlanrtTS string         `json:"planrtTS"` // Unix epoch as string
    SD       string         `json:"sD"`       // server date YYYYMMDD
    ST       string         `json:"sT"`       // server time HHMMSS
}

type hafasCommon struct {
    LocL  []hafasLocation `json:"locL"`
    ProdL []hafasProduct  `json:"prodL"`
    RemL  []hafasRemark   `json:"remL"`
}

type hafasLocation struct {
    Name     string    `json:"name"`
    ExtID    string    `json:"extId"`
    Crd      hafasCrd  `json:"crd"`
    PCls     int       `json:"pCls"`
    Type     string    `json:"type"`     // "S" for stop/station
    TZOffset int       `json:"TZOffset"` // timezone offset in minutes (e.g. 120 for CEST)
    Dist     int       `json:"dist"`     // distance in meters (only in LocGeoPos responses)
}

type hafasCrd struct {
    X int `json:"x"` // longitude * 1_000_000
    Y int `json:"y"` // latitude * 1_000_000
}

type hafasProduct struct {
    Name    string         `json:"name"`
    NameS   string         `json:"nameS"` // short name — prefer this for line name
    Cls     int            `json:"cls"`    // product class bitmask
    ProdCtx hafasProdCtx   `json:"prodCtx"`
}

type hafasProdCtx struct {
    CatCode string `json:"catCode"` // category code as string: "0"=suburban, "1"=subway, etc.
    CatOut  string `json:"catOut"`  // category output name, e.g. "S       ", "U       "
}

type hafasRemark struct {
    Type string `json:"type"` // "A" for attribute
    Code string `json:"code"`
    TxtN string `json:"txtN"`
}

type hafasJourney struct {
    JID     string      `json:"jid"`
    Date    string      `json:"date"`    // YYYYMMDD
    ProdX   int         `json:"prodX"`   // index into common.prodL
    DirTxt  string      `json:"dirTxt"`  // direction text
    Status  string      `json:"status"`  // "P" = planned
    StbStop hafasStbStop `json:"stbStop"` // station board stop (departure/arrival info)
}

type hafasStbStop struct {
    LocX  int    `json:"locX"`  // index into common.locL
    DProdX int   `json:"dProdX"` // departure product index (used for departures)
    AProdX int   `json:"aProdX"` // arrival product index (used for arrivals)

    // Departure fields (present when type=DEP)
    DTimeS string         `json:"dTimeS"` // scheduled departure HHMMSS
    DTimeR string         `json:"dTimeR"` // realtime departure HHMMSS (optional)
    DPltfS *hafasPlatform `json:"dPltfS"` // scheduled departure platform (optional)
    DCncl  bool           `json:"dCncl"`  // departure cancelled

    // Arrival fields (present when type=ARR)
    ATimeS string         `json:"aTimeS"` // scheduled arrival HHMMSS
    ATimeR string         `json:"aTimeR"` // realtime arrival HHMMSS (optional)
    APltfS *hafasPlatform `json:"aPltfS"` // scheduled arrival platform (optional)
    ACncl  bool           `json:"aCncl"`  // arrival cancelled
}

type hafasPlatform struct {
    Txt string `json:"txt"` // e.g. "4", "1 (U9)", "Pos. 1"
}

// -- LocMatch response --

type hafasLocMatchResult struct {
    Match struct {
        LocL []hafasLocation `json:"locL"`
    } `json:"match"`
}

// -- LocGeoPos response --

type hafasLocGeoPosResult struct {
    Common hafasCommon     `json:"common"`
    LocL   []hafasLocation `json:"locL"`
}
```

#### Core request method

```go
func (c *HAFASClient) do(ctx context.Context, method string, req interface{}) (*hafasSvcResult, error) {
    body, err := json.Marshal(hafasRequest{
        Lang: "en",
        SvcReqL: []hafasSvcRequest{{Meth: method, Req: req}},
        Client: hafasClientID{
            Type: "WEB",
            ID:   "VBB",
            Name: "VBB WebApp",
            L:    "vs_webapp_vbb",
        },
        Ver:  c.version,
        Auth: hafasAuth{Type: "AID", AID: c.authAID},
    })
    if err != nil {
        return nil, err
    }

    httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
    if err != nil {
        return nil, err
    }
    httpReq.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(httpReq)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("HAFAS returned status %d", resp.StatusCode)
    }

    var result hafasResponse
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    if result.Err != "OK" {
        return nil, fmt.Errorf("HAFAS top-level error: %s", result.Err)
    }
    if len(result.SvcResL) == 0 {
        return nil, fmt.Errorf("HAFAS returned empty svcResL")
    }
    if result.SvcResL[0].Err != "OK" {
        return nil, fmt.Errorf("HAFAS service error: %s", result.SvcResL[0].Err)
    }

    return &result.SvcResL[0], nil
}
```

#### Method: StationBoard

```go
func (c *HAFASClient) stationBoard(ctx context.Context, stopID string, boardType string, duration int) (*hafasStationBoardResult, error) {
    svc, err := c.do(ctx, "StationBoard", map[string]interface{}{
        "type":   boardType, // "DEP" or "ARR"
        "stbLoc": map[string]string{"lid": "A=1@L=" + stopID + "@"},
        "dur":    duration,
    })
    if err != nil {
        return nil, err
    }

    var res hafasStationBoardResult
    if err := json.Unmarshal(svc.Res, &res); err != nil {
        return nil, err
    }
    return &res, nil
}
```

#### Method: LocMatch

```go
func (c *HAFASClient) locMatch(ctx context.Context, query string, maxResults int) (*hafasLocMatchResult, error) {
    svc, err := c.do(ctx, "LocMatch", map[string]interface{}{
        "input": map[string]interface{}{
            "field":  "S",
            "loc":    map[string]interface{}{"name": query, "type": "S"},
            "maxLoc": maxResults,
        },
    })
    if err != nil {
        return nil, err
    }

    var res hafasLocMatchResult
    if err := json.Unmarshal(svc.Res, &res); err != nil {
        return nil, err
    }
    return &res, nil
}
```

#### Method: LocMatch by extId (for `/stops/{id}`)

```go
func (c *HAFASClient) locMatchByExtID(ctx context.Context, extID string) (*hafasLocMatchResult, error) {
    svc, err := c.do(ctx, "LocMatch", map[string]interface{}{
        "input": map[string]interface{}{
            "field": "S",
            "loc":   map[string]interface{}{"type": "S", "extId": extID},
        },
    })
    if err != nil {
        return nil, err
    }

    var res hafasLocMatchResult
    if err := json.Unmarshal(svc.Res, &res); err != nil {
        return nil, err
    }
    return &res, nil
}
```

#### Method: LocGeoPos (for `/locations/nearby`)

```go
func (c *HAFASClient) locGeoPos(ctx context.Context, lat, lon float64, maxDist, maxResults int) (*hafasLocGeoPosResult, error) {
    svc, err := c.do(ctx, "LocGeoPos", map[string]interface{}{
        "ring": map[string]interface{}{
            "cCrd":    map[string]int{"x": int(lon * 1_000_000), "y": int(lat * 1_000_000)},
            "maxDist": maxDist,
        },
        "maxLoc":   maxResults,
        "getStops": true,
    })
    if err != nil {
        return nil, err
    }

    var res hafasLocGeoPosResult
    if err := json.Unmarshal(svc.Res, &res); err != nil {
        return nil, err
    }
    return &res, nil
}
```

#### Helper: `parseHAFASTime`

HAFAS times are in `HHMMSS` format with a separate `YYYYMMDD` date. Times can exceed 24h for services crossing midnight (e.g. `250000` = 01:00:00 the next day). `tzOffset` is in minutes (e.g. 120 for CEST).

```go
func parseHAFASTime(date, timeStr string, tzOffset int) time.Time {
    if len(date) != 8 || len(timeStr) < 6 {
        return time.Time{}
    }

    year, _ := strconv.Atoi(date[0:4])
    month, _ := strconv.Atoi(date[4:6])
    day, _ := strconv.Atoi(date[6:8])

    hour, _ := strconv.Atoi(timeStr[0 : len(timeStr)-4])
    min, _ := strconv.Atoi(timeStr[len(timeStr)-4 : len(timeStr)-2])
    sec, _ := strconv.Atoi(timeStr[len(timeStr)-2:])

    loc := time.FixedZone("", tzOffset*60)
    t := time.Date(year, time.Month(month), day, hour, min, sec, 0, loc)
    return t
}
```

**Critical**: the hour can be >23 (e.g. `250000` means hour=25). `time.Date` in Go handles this correctly by rolling over to the next day automatically, so `time.Date(2026, 8, 22, 25, 0, 0, 0, loc)` produces `2026-08-23T01:00:00`.

When `tzOffset` is not available (e.g. in StationBoard responses where the location's `TZOffset` field is the source), use the `TZOffset` from the location referenced by `stbStop.locX`. If `TZOffset` is 0 or missing, default to `120` (CEST, the timezone VBB operates in during summer) — this is a reasonable default for Berlin.

#### Helper: `hafasProductName`

The `cls` field in HAFAS products is a bitmask. Use the lowest set bit to determine the primary product.

```go
func hafasProductName(cls int) string {
    switch {
    case cls&1 != 0:
        return "suburban"
    case cls&2 != 0:
        return "subway"
    case cls&4 != 0:
        return "tram"
    case cls&8 != 0:
        return "bus"
    case cls&16 != 0:
        return "ferry"
    case cls&32 != 0:
        return "express"
    case cls&64 != 0:
        return "regional"
    default:
        return ""
    }
}
```

#### Helper: `hafasProductsMap`

For location responses, transport.rest includes a `products` map showing which product types serve a stop. Derive this from the `pCls` bitmask on the location:

```go
func hafasProductsMap(pCls int) map[string]bool {
    return map[string]bool{
        "suburban":  pCls&1 != 0,
        "subway":    pCls&2 != 0,
        "tram":      pCls&4 != 0,
        "bus":       pCls&8 != 0,
        "ferry":     pCls&16 != 0,
        "express":   pCls&32 != 0,
        "regional":  pCls&64 != 0,
    }
}
```

### 2.2 New file: `hafas_test.go`

Write unit tests for:

1. `parseHAFASTime` — normal case, midnight rollover (hour >23), missing fields return zero time
2. `hafasProductName` — all 7 products + cls=0 returns ""
3. `hafasProductsMap` — pCls=67 (1+2+64 = suburban+subway+regional) returns correct map
4. `HAFASClient.do` — mock HTTP server returning a valid HAFAS response envelope; verify it returns the `Res` field; mock returning `err: "FAIL"` → verify it returns an error

---

## Part 3: HAFAS translation to transport.rest JSON

### 3.1 New file: `hafas_translate.go`

This file contains functions that translate HAFAS response structs into transport.rest-compatible JSON bytes.

#### Transport.rest response shapes

For reference, these are the JSON shapes that transport.rest returns and that consumers expect. The translated HAFAS responses must produce these exact shapes.

**Departures** (`/stops/{id}/departures`):
```json
{
    "departures": [
        {
            "tripId": "1|41638|1|86|22082026",
            "stop": {
                "type": "stop",
                "id": "900100003",
                "name": "S+U Alexanderplatz Bhf (Berlin)",
                "location": {
                    "type": "location",
                    "id": "900100003",
                    "latitude": 52.521508,
                    "longitude": 13.411267
                },
                "products": {
                    "suburban": true,
                    "subway": true,
                    "tram": false,
                    "bus": true,
                    "ferry": false,
                    "express": false,
                    "regional": true
                }
            },
            "when": "2026-08-22T23:23:00+02:00",
            "plannedWhen": "2026-08-22T23:20:00+02:00",
            "delay": 180,
            "platform": "Pos. 1",
            "direction": "S+U Zoologischer Garten via Potsdamer Platz",
            "line": {
                "type": "line",
                "id": "200",
                "name": "200",
                "mode": "bus",
                "product": "bus"
            },
            "remarks": [],
            "cancelled": false
        }
    ],
    "realtimeDataUpdatedAt": 1787433776
}
```

**Arrivals** (`/stops/{id}/arrivals`) — same shape but:
- Top-level key is `"arrivals"` instead of `"departures"`
- Each item has `"provenance"` instead of `"direction"`

**Locations** (`/locations`):
```json
[
    {
        "type": "stop",
        "id": "900100003",
        "name": "S+U Alexanderplatz Bhf (Berlin)",
        "location": {
            "type": "location",
            "id": "900100003",
            "latitude": 52.521508,
            "longitude": 13.411267
        },
        "products": {
            "suburban": true,
            "subway": true,
            "tram": false,
            "bus": true,
            "ferry": false,
            "express": false,
            "regional": true
        }
    }
]
```

**Nearby locations** (`/locations/nearby`) — same as locations but each object includes `"distance": <meters>`.

**Stop** (`/stops/{id}`) — same as a single location object (not wrapped in an array).

#### Translation function: `translateDepartures`

```go
func translateDepartures(res *hafasStationBoardResult) ([]byte, error)
```

Produces `{"departures":[...], "realtimeDataUpdatedAt": N}`.

For each journey in `res.JnyL`:
1. Resolve `loc := res.Common.LocL[jny.StbStop.LocX]` — bounds-check the index
2. Resolve `prod := res.Common.ProdL[jny.ProdX]` — bounds-check the index
3. Get timezone: `tzOffset := loc.TZOffset` (default to 120 if 0)
4. Parse scheduled time: `plannedWhen := parseHAFASTime(jny.Date, jny.StbStop.DTimeS, tzOffset)`
5. Parse realtime time: if `jny.StbStop.DTimeR != ""`, `when := parseHAFASTime(jny.Date, jny.StbStop.DTimeR, tzOffset)`, else `when = plannedWhen`
6. Compute delay: if `jny.StbStop.DTimeR != ""`, `delay := int(when.Sub(plannedWhen).Seconds())`, else delay is `nil` (use `*int` and `json:"delay"` with `omitempty` — but transport.rest uses `null` for no delay, so use a pointer: `Delay *int`)
7. Format `when` and `plannedWhen` as RFC3339 strings
8. If cancelled (`jny.StbStop.DCncl`), set `when` to `null` (empty string in the JSON), keep `plannedWhen`
9. Line name: use `prod.NameS` if non-empty, else `prod.Name`
10. Product: `hafasProductName(prod.Cls)`
11. Mode: derive from product — `"train"` for suburban/subway/tram/express/regional, `"bus"` for bus, `"watercraft"` for ferry
12. Platform: `jny.StbStop.DPltfS.Txt` if present, else `null`
13. `tripId`: use `jny.JID` (it's already a string identifier)
14. Direction: `jny.DirTxt`
15. `remarks`: empty array `[]` (we are not translating HAFAS remarks in this version)
16. `realtimeDataUpdatedAt`: parse `res.PlanrtTS` as int64 (it's a Unix epoch as string)

Build a struct and `json.Marshal` it. Use these struct definitions:

```go
type restDeparturesResponse struct {
    Departures            []restDeparture `json:"departures"`
    RealtimeDataUpdatedAt int64           `json:"realtimeDataUpdatedAt"`
}

type restDeparture struct {
    TripID     string       `json:"tripId"`
    Stop       restStop     `json:"stop"`
    When       *string      `json:"when"`        // null when cancelled
    PlannedWhen string      `json:"plannedWhen"`
    Delay      *int         `json:"delay"`       // null when no realtime data
    Platform   *string      `json:"platform"`    // null when unknown
    Direction  string       `json:"direction"`
    Line       restLine     `json:"line"`
    Remarks    []struct{}   `json:"remarks"`
    Cancelled  bool         `json:"cancelled"`
}

type restArrival struct {
    TripID     string       `json:"tripId"`
    Stop       restStop     `json:"stop"`
    When       *string      `json:"when"`
    PlannedWhen string      `json:"plannedWhen"`
    Delay      *int         `json:"delay"`
    Platform   *string      `json:"platform"`
    Provenance string       `json:"provenance"`  // instead of "direction"
    Line       restLine     `json:"line"`
    Remarks    []struct{}   `json:"remarks"`
    Cancelled  bool         `json:"cancelled"`
}

type restStop struct {
    Type     string           `json:"type"`     // always "stop"
    ID       string           `json:"id"`
    Name     string           `json:"name"`
    Location restLocation     `json:"location"`
    Products map[string]bool  `json:"products"`
}

type restLocation struct {
    Type      string  `json:"type"`      // always "location"
    ID        string  `json:"id"`
    Latitude  float64 `json:"latitude"`
    Longitude float64 `json:"longitude"`
}

type restLine struct {
    Type    string `json:"type"`    // always "line"
    ID      string `json:"id"`
    Name    string `json:"name"`
    Mode    string `json:"mode"`
    Product string `json:"product"`
}
```

Helper for mode:
```go
func hafasProductMode(product string) string {
    switch product {
    case "bus":
        return "bus"
    case "ferry":
        return "watercraft"
    default:
        return "train"
    }
}
```

Helper to build a `restStop` from a `hafasLocation`:
```go
func hafasLocationToRestStop(loc hafasLocation) restStop {
    return restStop{
        Type: "stop",
        ID:   loc.ExtID,
        Name: loc.Name,
        Location: restLocation{
            Type:      "location",
            ID:        loc.ExtID,
            Latitude:  float64(loc.Crd.Y) / 1_000_000,
            Longitude: float64(loc.Crd.X) / 1_000_000,
        },
        Products: hafasProductsMap(loc.PCls),
    }
}
```

#### Translation function: `translateArrivals`

```go
func translateArrivals(res *hafasStationBoardResult) ([]byte, error)
```

Identical to `translateDepartures` except:
- Top-level key is `"arrivals"` instead of `"departures"`
- Use `ATimeS`/`ATimeR` instead of `DTimeS`/`DTimeR`
- Use `APltfS` instead of `DPltfS`
- Use `ACncl` instead of `DCncl`
- Each item has `"provenance"` (= `jny.DirTxt`) instead of `"direction"`

#### Translation function: `translateLocations`

```go
func translateLocations(locs []hafasLocation) ([]byte, error)
```

Produces a JSON array of location objects. For each `hafasLocation`:
```go
hafasLocationToRestStop(loc)
```

Marshal the slice and return.

#### Translation function: `translateNearbyLocations`

```go
func translateNearbyLocations(locs []hafasLocation) ([]byte, error)
```

Same as `translateLocations` but uses a struct that also includes `distance`:

```go
type restNearbyStop struct {
    restStop
    Distance int `json:"distance"`
}
```

Set `Distance` from `loc.Dist`.

#### Translation function: `translateStop`

```go
func translateStop(loc hafasLocation) ([]byte, error)
```

Produces a single stop object (not wrapped in an array). Just marshal `hafasLocationToRestStop(loc)`.

### 3.2 New file: `hafas_translate_test.go`

Test each translation function with manually constructed HAFAS structs. Verify:

1. `translateDepartures` — build a `hafasStationBoardResult` with 2 journeys (one with realtime, one without); verify JSON contains correct `when`, `plannedWhen`, `delay`, line, product, stop, direction, `realtimeDataUpdatedAt`
2. `translateDepartures` with cancelled journey — verify `when` is `null`, `cancelled` is `true`
3. `translateDepartures` with >24h time — verify correct date rollover
4. `translateArrivals` — verify `arrivals` key and `provenance` field
5. `translateLocations` — verify array of stop objects with correct coordinates and products
6. `translateNearbyLocations` — verify `distance` field present
7. `translateStop` — verify single object (not array)
8. Verify that `translateDepartures` output can be parsed by `departuresExpiry` from `expiry.go` — this confirms cache expiry compatibility

---

## Part 4: HAFAS DataProvider methods

### 4.1 Add DataProvider methods to `HAFASClient`

Add these methods to `HAFASClient` in `hafas.go`. Each has the `DataProvider` signature and can be passed directly to handler constructors.

#### `Departures`

```go
func (c *HAFASClient) Departures(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
    path := routePath(r)
    stopID := r.PathValue("id")
    if stopID == "" {
        return nil, nil, fmt.Errorf("missing stop ID")
    }

    duration := 10 // transport.rest default
    if d := r.URL.Query().Get("duration"); d != "" {
        if v, err := strconv.Atoi(d); err == nil {
            duration = v
        }
    }

    start := time.Now()
    res, err := c.stationBoard(ctx, stopID, "DEP", duration)
    if err != nil {
        metrics.UpstreamErrorsTotal.WithLabelValues("hafas", r.Method, path, errorReason(err)).Inc()
        return nil, nil, err
    }
    metrics.UpstreamRequestDuration.WithLabelValues("hafas", r.Method, path).Observe(time.Since(start).Seconds())
    metrics.UpstreamRequestsTotal.WithLabelValues("hafas", r.Method, path, "200").Inc()

    body, err := translateDepartures(res)
    if err != nil {
        return nil, nil, err
    }

    header := http.Header{}
    header.Set("Content-Type", "application/json")
    return body, header, nil
}
```

#### `Arrivals`

Same as `Departures` but calls `stationBoard(ctx, stopID, "ARR", duration)` and `translateArrivals`.

#### `Locations`

```go
func (c *HAFASClient) Locations(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
    path := routePath(r)
    query := r.URL.Query().Get("query")
    if query == "" {
        return nil, nil, fmt.Errorf("missing query parameter")
    }

    results := 5 // reasonable default
    if v := r.URL.Query().Get("results"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            results = n
        }
    }

    start := time.Now()
    res, err := c.locMatch(ctx, query, results)
    if err != nil {
        metrics.UpstreamErrorsTotal.WithLabelValues("hafas", r.Method, path, errorReason(err)).Inc()
        return nil, nil, err
    }
    metrics.UpstreamRequestDuration.WithLabelValues("hafas", r.Method, path).Observe(time.Since(start).Seconds())
    metrics.UpstreamRequestsTotal.WithLabelValues("hafas", r.Method, path, "200").Inc()

    body, err := translateLocations(res.Match.LocL)
    if err != nil {
        return nil, nil, err
    }

    header := http.Header{}
    header.Set("Content-Type", "application/json")
    return body, header, nil
}
```

#### `Nearby`

```go
func (c *HAFASClient) Nearby(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
    path := routePath(r)
    q := r.URL.Query()
    latStr := q.Get("latitude")
    lonStr := q.Get("longitude")
    if latStr == "" || lonStr == "" {
        return nil, nil, fmt.Errorf("missing latitude/longitude")
    }

    lat, err := strconv.ParseFloat(latStr, 64)
    if err != nil {
        return nil, nil, err
    }
    lon, err := strconv.ParseFloat(lonStr, 64)
    if err != nil {
        return nil, nil, err
    }

    distance := 1000 // default 1km
    if v := q.Get("distance"); v != "" {
        if d, err := strconv.Atoi(v); err == nil {
            distance = d
        }
    }
    results := 8
    if v := q.Get("results"); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            results = n
        }
    }

    start := time.Now()
    res, err := c.locGeoPos(ctx, lat, lon, distance, results)
    if err != nil {
        metrics.UpstreamErrorsTotal.WithLabelValues("hafas", r.Method, path, errorReason(err)).Inc()
        return nil, nil, err
    }
    metrics.UpstreamRequestDuration.WithLabelValues("hafas", r.Method, path).Observe(time.Since(start).Seconds())
    metrics.UpstreamRequestsTotal.WithLabelValues("hafas", r.Method, path, "200").Inc()

    body, err := translateNearbyLocations(res.LocL)
    if err != nil {
        return nil, nil, err
    }

    header := http.Header{}
    header.Set("Content-Type", "application/json")
    return body, header, nil
}
```

#### `Stop`

```go
func (c *HAFASClient) Stop(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
    path := routePath(r)
    stopID := r.PathValue("id")
    if stopID == "" {
        return nil, nil, fmt.Errorf("missing stop ID")
    }

    start := time.Now()
    res, err := c.locMatchByExtID(ctx, stopID)
    if err != nil {
        metrics.UpstreamErrorsTotal.WithLabelValues("hafas", r.Method, path, errorReason(err)).Inc()
        return nil, nil, err
    }
    metrics.UpstreamRequestDuration.WithLabelValues("hafas", r.Method, path).Observe(time.Since(start).Seconds())
    metrics.UpstreamRequestsTotal.WithLabelValues("hafas", r.Method, path, "200").Inc()

    if len(res.Match.LocL) == 0 {
        return nil, nil, fmt.Errorf("stop not found: %s", stopID)
    }

    body, err := translateStop(res.Match.LocL[0])
    if err != nil {
        return nil, nil, err
    }

    header := http.Header{}
    header.Set("Content-Type", "application/json")
    return body, header, nil
}
```

---

## Part 5: Wire HAFAS into the proxy

### 5.1 Update `main.go`

Add HAFAS client creation from env vars:

```go
const (
    DefaultListenAddr       = ":8080"
    DefaultTimeout          = 10 * time.Second
    DefaultStaticCacheSize  = 512
    DefaultDynamicCacheSize = 2048
    DefaultHAFASVersion     = "1.45"
    upstreamURL             = "https://v6.vbb.transport.rest"
)
```

In `main()`, after flag parsing and before `newMux`:

```go
var hafas *HAFASClient
if endpoint, aid := os.Getenv("HAFAS_ENDPOINT"), os.Getenv("HAFAS_AUTH_AID"); endpoint != "" && aid != "" {
    version := os.Getenv("HAFAS_VERSION")
    if version == "" {
        version = DefaultHAFASVersion
    }
    hafasClient := &http.Client{Timeout: config.Timeout}
    hafas = NewHAFASClient(hafasClient, endpoint, aid, version)
    slog.Info("HAFAS fallback enabled", "endpoint", endpoint)
} else {
    slog.Info("HAFAS fallback disabled (set HAFAS_ENDPOINT and HAFAS_AUTH_AID to enable)")
}

mux := newMux(upstreamURL, config.Timeout, config.StaticCacheSize, config.DynamicCacheSize, metrics, hafas)
```

Add `"log/slog"` to imports if not already there (it is).

Update the slog line to also include HAFAS info:
```go
slog.Info("starting server", "listenAddr", config.ListenAddr, "timeout", config.Timeout,
    "staticCacheSize", config.StaticCacheSize, "dynamicCacheSize", config.DynamicCacheSize,
    "hafas", hafas != nil)
```

### 5.2 Update `mux.go`

Change `newMux` signature to accept `*HAFASClient`:

```go
func newMux(upstreamURL string, timeout time.Duration, staticCap, dynamicCap int, metrics *Metrics, hafas *HAFASClient) *http.ServeMux {
```

Build provider lists:

```go
    rest := &TransportRESTClient{Client: client, Upstream: upstreamURL}
    restOnly := []DataProvider{rest.Forward}

    // Build provider lists with optional HAFAS fallback.
    depProviders := restOnly
    arrProviders := restOnly
    locProviders := restOnly
    nearbyProviders := restOnly
    stopProviders := restOnly
    if hafas != nil {
        depProviders = []DataProvider{rest.Forward, hafas.Departures}
        arrProviders = []DataProvider{rest.Forward, hafas.Arrivals}
        locProviders = []DataProvider{rest.Forward, hafas.Locations}
        nearbyProviders = []DataProvider{rest.Forward, hafas.Nearby}
        stopProviders = []DataProvider{rest.Forward, hafas.Stop}
    }
```

Then use them in route registration:

```go
    mux.HandleFunc("GET /stops/reachable-from", handleReachableFrom(restOnly, dynamicCache, metrics))
    mux.HandleFunc("GET /stops/{id}/departures", handleDepartures(depProviders, dynamicCache, metrics))
    mux.HandleFunc("GET /stops/{id}/arrivals", handleArrivals(arrProviders, dynamicCache, metrics))
    mux.HandleFunc("GET /stops/{id}", handleStop(stopProviders, staticCache, metrics))

    mux.HandleFunc("GET /journeys/{ref}", handleRefreshJourney(restOnly, dynamicCache, metrics))
    mux.HandleFunc("GET /journeys", handleJourneys(restOnly, dynamicCache, metrics))

    mux.HandleFunc("GET /trips/{id}", handleTrip(restOnly, dynamicCache, metrics))
    mux.HandleFunc("GET /trips", handleTrips(restOnly, dynamicCache, metrics))

    mux.HandleFunc("GET /locations/nearby", handleNearby(nearbyProviders, staticCache, metrics))
    mux.HandleFunc("GET /locations", handleLocations(locProviders, staticCache, metrics))

    mux.HandleFunc("GET /radar", handleRadar(client, upstreamURL, metrics))

    mux.HandleFunc("GET /stations/{id}", handleStation(restOnly, staticCache, metrics))
    mux.HandleFunc("GET /stations", handleStations(restOnly, staticCache, metrics))

    mux.HandleFunc("GET /lines/{id}", handleLine(restOnly, staticCache, metrics))
    mux.HandleFunc("GET /lines", handleLines(restOnly, staticCache, metrics))

    mux.HandleFunc("GET /shapes/{id}", handleShape(client, upstreamURL, staticCache, metrics))
    mux.HandleFunc("GET /maps/{type}", handleMap(client, upstreamURL, staticCache, metrics))

    mux.HandleFunc("GET /compact/departures", handleCompactDepartures(client, upstreamURL, dynamicCache, metrics, hafas))
    mux.HandleFunc("GET /healthz", handleHealthz)
```

### 5.3 Update test helpers

**File: `testhelpers_test.go`**

All three helpers call `newMux` — add `nil` as the last argument (no HAFAS in existing tests):

```go
func newTestStack(upstreamHandler http.Handler, timeout time.Duration) (srvURL string, cleanup func()) {
    upstream := httptest.NewServer(upstreamHandler)
    mux := newMux(upstream.URL, timeout, 0, 0, newTestMetrics(), nil)
    // ...
}

func newUnreachableStack(timeout time.Duration) (srvURL string, cleanup func()) {
    // ...
    mux := newMux(upstreamURL, timeout, 0, 0, newTestMetrics(), nil)
    // ...
}

func newCachedTestStack(upstreamHandler http.Handler, timeout time.Duration, staticCap, dynamicCap int) (srvURL string, cleanup func()) {
    // ...
    mux := newMux(upstream.URL, timeout, staticCap, dynamicCap, newTestMetrics(), nil)
    // ...
}
```

---

## Part 6: Compact departures HAFAS fallback

### 6.1 Update `handle_compact_departures.go`

Change `handleCompactDepartures` signature to accept `*HAFASClient`:

```go
func handleCompactDepartures(client *http.Client, upstream string, cache *Cache, metrics *Metrics, hafas *HAFASClient) http.HandlerFunc {
```

In the per-stop goroutine, after `fetchCompactBoard` fails, try HAFAS if available:

```go
go func(i int, id string) {
    defer wg.Done()
    board, updatedAt, err := fetchCompactBoard(r.Context(), client, upstream, id, duration, metrics)
    if err != nil && hafas != nil {
        board, updatedAt, err = fetchCompactBoardFromHAFAS(r.Context(), hafas, id, duration, metrics)
    }
    results[i] = compactFetchResult{board: board, realtimeDataUpdatedAt: updatedAt, err: err}
}(i, id)
```

### 6.2 Update `fetchCompactBoard` metric labels

The existing `fetchCompactBoard` function already records metrics directly (not via `TransportRESTClient.Forward`). Update all its `.WithLabelValues(...)` calls to include `"transport_rest"` as the first argument:

```go
// In fetchCompactBoard, change:
metrics.UpstreamErrorsTotal.WithLabelValues(http.MethodGet, compactRoutePath, errorReason(err)).Inc()
// to:
metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", http.MethodGet, compactRoutePath, errorReason(err)).Inc()

// Same for the other two:
metrics.UpstreamRequestDuration.WithLabelValues("transport_rest", http.MethodGet, compactRoutePath).Observe(...)
metrics.UpstreamRequestsTotal.WithLabelValues("transport_rest", http.MethodGet, compactRoutePath, strconv.Itoa(resp.StatusCode)).Inc()
metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", http.MethodGet, compactRoutePath, httpErrorReason(resp.StatusCode)).Inc()
```

### 6.3 Add `fetchCompactBoardFromHAFAS`

Add this function to `handle_compact_departures.go`:

```go
func fetchCompactBoardFromHAFAS(ctx context.Context, hafas *HAFASClient, id string, duration int, metrics *Metrics) (*compactBoard, int64, error) {
    start := time.Now()
    res, err := hafas.stationBoard(ctx, id, "DEP", duration)
    if err != nil {
        metrics.UpstreamErrorsTotal.WithLabelValues("hafas", http.MethodGet, compactRoutePath, errorReason(err)).Inc()
        return nil, 0, err
    }
    metrics.UpstreamRequestDuration.WithLabelValues("hafas", http.MethodGet, compactRoutePath).Observe(time.Since(start).Seconds())
    metrics.UpstreamRequestsTotal.WithLabelValues("hafas", http.MethodGet, compactRoutePath, "200").Inc()

    return transformHAFASCompactBoard(id, res), hafasPlanrtTS(res.PlanrtTS), nil
}
```

### 6.4 Add `transformHAFASCompactBoard`

This is a pure function (testable without HTTP) that maps HAFAS StationBoard → `compactBoard`:

```go
func transformHAFASCompactBoard(id string, res *hafasStationBoardResult) *compactBoard {
    board := &compactBoard{
        ID:         id,
        Name:       id,
        Departures: []compactDeparture{},
    }

    cutoff := time.Now().Add(-compactPastGrace)
    nameSet := false

    for _, jny := range res.JnyL {
        // Bounds check
        if jny.StbStop.LocX < 0 || jny.StbStop.LocX >= len(res.Common.LocL) {
            continue
        }
        if jny.ProdX < 0 || jny.ProdX >= len(res.Common.ProdL) {
            continue
        }

        loc := res.Common.LocL[jny.StbStop.LocX]
        prod := res.Common.ProdL[jny.ProdX]

        if !nameSet && loc.Name != "" {
            board.Name = loc.Name
            nameSet = true
        }

        tzOffset := loc.TZOffset
        if tzOffset == 0 {
            tzOffset = 120
        }

        plannedWhen := parseHAFASTime(jny.Date, jny.StbStop.DTimeS, tzOffset)
        when := plannedWhen
        if jny.StbStop.DTimeR != "" {
            when = parseHAFASTime(jny.Date, jny.StbStop.DTimeR, tzOffset)
        }

        t := when
        if t.IsZero() || t.Before(cutoff) {
            continue
        }

        lineName := prod.NameS
        if lineName == "" {
            lineName = prod.Name
        }
        if lineName == "" {
            lineName = "?"
        }

        delay := 0
        if jny.StbStop.DTimeR != "" {
            delaySec := when.Sub(plannedWhen).Seconds()
            delay = int(math.Floor(delaySec/60.0 + 0.5))
        }

        cancelled := 0
        if jny.StbStop.DCncl {
            cancelled = 1
        }

        board.Departures = append(board.Departures, compactDeparture{
            Line:      lineName,
            Product:   hafasProductName(prod.Cls),
            Direction: compactStripDirection(jny.DirTxt),
            Time:      t.Unix(),
            Delay:     delay,
            Cancelled: cancelled,
            Warning:   0, // not translating HAFAS HIM messages
        })
    }

    slices.SortStableFunc(board.Departures, func(a, b compactDeparture) int {
        return cmp.Compare(a.Time, b.Time)
    })

    return board
}
```

### 6.5 Add `hafasPlanrtTS` helper

```go
func hafasPlanrtTS(s string) int64 {
    v, _ := strconv.ParseInt(s, 10, 64)
    return v
}
```

---

## Part 7: Verification

### 7.1 Build

```bash
go build ./...
```

### 7.2 Existing tests

```bash
go test ./...
```

All existing tests must pass. The refactor (Parts 1, 5.3) changes no behavior — it only restructures how data flows to the same outcome.

### 7.3 New unit tests

Run the new test files:

```bash
go test -run TestHAFAS ./...
go test -run TestTranslate ./...
```

### 7.4 Manual smoke test

With transport.rest still down, run the proxy with HAFAS enabled:

```bash
HAFAS_ENDPOINT=https://fahrinfo.vbb.de/bin/mgate.exe \
HAFAS_AUTH_AID=hafas-vbb-webapp \
go run main.go
```

Then test:

```bash
# Departures
curl -s 'http://localhost:8080/stops/900100003/departures?duration=30' | jq '.departures[:2]'

# Arrivals
curl -s 'http://localhost:8080/stops/900100003/arrivals?duration=30' | jq '.arrivals[:2]'

# Locations
curl -s 'http://localhost:8080/locations?query=Alexanderplatz&results=3' | jq '.[:2]'

# Nearby
curl -s 'http://localhost:8080/locations/nearby?latitude=52.521508&longitude=13.411267&distance=500' | jq '.[:2]'

# Stop
curl -s 'http://localhost:8080/stops/900100003' | jq '{type, id, name}'

# Compact departures
curl -s 'http://localhost:8080/compact/departures?stops=900100003&duration=30&limit=3' | jq .
```

All should return real HAFAS data despite transport.rest being down.

### 7.5 Verify per-upstream metrics

After the smoke test, check that metrics are separated by upstream:

```bash
curl -s 'http://localhost:8080/metrics' | grep upstream_requests_total
```

Expected output should show the `upstream` label:

```
upstream_requests_total{upstream="transport_rest",method="GET",path="/stops/{id}/departures",status="..."} ...
upstream_requests_total{upstream="hafas",method="GET",path="/stops/{id}/departures",status="200"} ...
```

Verify that:
- transport.rest failures appear as `upstream_errors_total{upstream="transport_rest", ...}`
- HAFAS successes appear as `upstream_requests_total{upstream="hafas", ...}`
- HAFAS response times appear as `upstream_request_duration_seconds{upstream="hafas", ...}`
- `fallback_responses_total` has no `upstream` label (unchanged)

### 7.6 Lint

```bash
golangci-lint run
```

---

## Summary of all files changed/created

| File | Action |
|---|---|
| `metrics.go` | Modify: add `"upstream"` label to `UpstreamRequestsTotal`, `UpstreamRequestDuration`, `UpstreamErrorsTotal` |
| `proxy.go` | Modify: add `DataProvider` type, `TransportRESTClient` struct with `Forward` method (records metrics with `"transport_rest"` label); refactor `newStandardHandler` to accept `[]DataProvider`; update `newPassthroughHandler` metric calls with `"transport_rest"` label |
| `hafas.go` | Create: `HAFASClient`, request/response structs, `do()`, `stationBoard()`, `locMatch()`, `locMatchByExtID()`, `locGeoPos()`, `parseHAFASTime()`, `hafasProductName()`, `hafasProductsMap()`, `Departures()`, `Arrivals()`, `Locations()`, `Nearby()`, `Stop()` |
| `hafas_translate.go` | Create: `translateDepartures()`, `translateArrivals()`, `translateLocations()`, `translateNearbyLocations()`, `translateStop()`, REST response structs, `hafasLocationToRestStop()`, `hafasProductMode()` |
| `hafas_test.go` | Create: tests for `parseHAFASTime`, `hafasProductName`, `HAFASClient.do` |
| `hafas_translate_test.go` | Create: tests for all translate functions |
| `handle_departures.go` | Modify: signature `(providers []DataProvider, cache, metrics)` |
| `handle_arrivals.go` | Modify: same |
| `handle_journeys.go` | Modify: both functions |
| `handle_trips.go` | Modify: both functions |
| `handle_locations.go` | Modify: both functions |
| `handle_stops.go` | Modify: both functions |
| `handle_stations.go` | Modify: both functions |
| `handle_lines.go` | Modify: both functions |
| `handle_compact_departures.go` | Modify: add `hafas *HAFASClient` param, `fetchCompactBoardFromHAFAS` (with `"hafas"` metrics), `transformHAFASCompactBoard`; update `fetchCompactBoard` metric calls with `"transport_rest"` label |
| `handle_maps.go` | Modify: update metric calls with `"transport_rest"` label |
| `handle_shapes.go` | Modify: update metric calls with `"transport_rest"` label |
| `mux.go` | Modify: add `hafas *HAFASClient` param, build provider lists |
| `main.go` | Modify: add HAFAS env var handling, pass `hafas` to `newMux` |
| `testhelpers_test.go` | Modify: pass `nil` as HAFAS arg to `newMux` calls |
