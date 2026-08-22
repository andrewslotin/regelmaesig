package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Constants for the /compact/departures endpoint. Prefixed "compact" to avoid
// collisions with the rest of package main.
const (
	compactRoutePath = "/compact/departures"
	compactUserAgent = "regelmaesig/1.0 (+https://github.com/andrewslotin/regelmaesig)"

	compactMaxStops       = 4
	compactMaxIDLen       = 12
	compactDirectionRunes = 24
	compactPastGrace      = 30 * time.Second

	compactDefaultDuration = 30
	compactMinDuration     = 10
	compactMaxDuration     = 120

	compactDefaultLimit = 6
	compactMinLimit     = 1
	compactMaxLimit     = 10
)

// Error response bodies, shared with tests.
const (
	compactErrBadRequest = `{"error":"bad_request"}`
	compactErrUpstream   = `{"error":"upstream"}`
	compactErrInternal   = `{"error":"internal"}`
)

// -- wire response schema (field order is significant: encoding/json marshals
// struct fields in declaration order, and tests assert exact bodies) --

type compactResponse struct {
	AsOf    int64          `json:"asOf"`
	Partial int            `json:"partial"`
	Boards  []compactBoard `json:"boards"`
}

type compactBoard struct {
	ID         string             `json:"id"`
	Name       string             `json:"n"`
	Departures []compactDeparture `json:"d"`
}

type compactDeparture struct {
	Line      string `json:"l"`
	Product   string `json:"p"`
	Direction string `json:"dir"`
	Time      int64  `json:"t"`
	Delay     int    `json:"dl"`
	Cancelled int    `json:"c"`
	Warning   int    `json:"w"`
}

// -- upstream decode schema (only the fields this endpoint needs) --

type compactUpstreamResponse struct {
	Departures            []compactUpstreamDeparture `json:"departures"`
	RealtimeDataUpdatedAt int64                      `json:"realtimeDataUpdatedAt"`
}

type compactUpstreamDeparture struct {
	When        string   `json:"when"`
	PlannedWhen string   `json:"plannedWhen"`
	Delay       *float64 `json:"delay"`
	Cancelled   bool     `json:"cancelled"`
	Direction   string   `json:"direction"`
	Line        struct {
		Name    string `json:"name"`
		Product string `json:"product"`
	} `json:"line"`
	Stop struct {
		Name string `json:"name"`
	} `json:"stop"`
	Remarks []struct {
		Type string `json:"type"`
	} `json:"remarks"`
}

// handleCompactDepartures serves a compact, transformed multi-stop departure
// board for lightweight clients (watch apps, widgets, e-ink displays).
// Unlike newStandardHandler/newPassthroughHandler, it fans
// out to N synthesized upstream requests (one per requested stop) and
// transforms the payload; the cache is used purely as a stale-if-error
// fallback when every fetch fails — there is no "serve from cache while
// fresh" fast path.
func handleCompactDepartures(client *http.Client, upstream string, cache *Cache, metrics *Metrics, hafas *HAFASClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stops, ok := parseCompactStops(r.URL.Query().Get("stops"))
		if !ok {
			writeCompactJSON(w, http.StatusBadRequest, []byte(compactErrBadRequest))
			return
		}
		duration := parseCompactClamped(r.URL.Query().Get("duration"), compactDefaultDuration, compactMinDuration, compactMaxDuration)
		limit := parseCompactClamped(r.URL.Query().Get("limit"), compactDefaultLimit, compactMinLimit, compactMaxLimit)

		key := compactCacheKey(stops, duration, limit)

		// One goroutine per requested stop, each writing only to its own
		// index of results — no mutex needed since indices never overlap.
		results := make([]compactFetchResult, len(stops))
		var wg sync.WaitGroup
		wg.Add(len(stops))
		for i, id := range stops {
			go func(i int, id string) {
				defer wg.Done()
				board, updatedAt, err := fetchCompactBoard(r.Context(), client, upstream, id, duration, metrics)
				if err != nil && hafas != nil {
					board, updatedAt, err = fetchCompactBoardFromHAFAS(r.Context(), hafas, id, duration, metrics)
				}
				results[i] = compactFetchResult{board: board, realtimeDataUpdatedAt: updatedAt, err: err}
			}(i, id)
		}
		wg.Wait()

		boards := make([]compactBoard, 0, len(stops))
		var asOf int64
		failed := false
		for _, res := range results {
			if res.err != nil {
				failed = true
				continue
			}
			board := *res.board
			if len(board.Departures) > limit {
				board.Departures = board.Departures[:limit]
			}
			boards = append(boards, board)
			if res.realtimeDataUpdatedAt > asOf {
				asOf = res.realtimeDataUpdatedAt
			}
		}

		if len(boards) == 0 {
			if entry, ok := cache.Get(key); ok {
				w.Header().Set("X-Cache", "HIT")
				writeCompactJSON(w, http.StatusOK, entry.body)
				return
			}
			metrics.FallbackResponsesTotal.WithLabelValues(http.MethodGet, compactRoutePath).Inc()
			writeCompactJSON(w, http.StatusBadGateway, []byte(compactErrUpstream))
			return
		}

		if asOf == 0 {
			asOf = time.Now().Unix()
		}
		partial := 0
		if failed {
			partial = 1
		}

		body, err := json.Marshal(compactResponse{AsOf: asOf, Partial: partial, Boards: boards})
		if err != nil {
			writeCompactJSON(w, http.StatusInternalServerError, []byte(compactErrInternal))
			return
		}

		// Cache only complete, non-empty responses; expiry is the latest
		// departure time in the body, so a body with no departures at all
		// (expiresAt zero) is never stored either.
		if partial == 0 {
			if expiresAt := compactCacheExpiry(boards); expiresAt.After(time.Now()) {
				cache.Set(key, &cacheEntry{
					statusCode: http.StatusOK,
					body:       body,
					expiresAt:  expiresAt,
				})
			}
		}

		writeCompactJSON(w, http.StatusOK, body)
	}
}

// compactFetchResult carries the outcome of a single per-stop upstream fetch.
type compactFetchResult struct {
	board                 *compactBoard
	realtimeDataUpdatedAt int64
	err                   error
}

// fetchCompactBoard fetches and transforms a single stop's departure board.
func fetchCompactBoard(ctx context.Context, client *http.Client, upstream, id string, duration int, metrics *Metrics) (*compactBoard, int64, error) {
	url := upstream + "/stops/" + id + "/departures?duration=" + strconv.Itoa(duration) +
		"&results=20&remarks=true&language=en&suburban=true&subway=true&tram=true&bus=true&ferry=true&regional=true&express=false&pretty=false"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", compactUserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", http.MethodGet, compactRoutePath, errorReason(err)).Inc()
		return nil, 0, err
	}
	defer resp.Body.Close() //nolint:errcheck

	metrics.UpstreamRequestDuration.WithLabelValues("transport_rest", http.MethodGet, compactRoutePath).Observe(time.Since(start).Seconds())
	metrics.UpstreamRequestsTotal.WithLabelValues("transport_rest", http.MethodGet, compactRoutePath, strconv.Itoa(resp.StatusCode)).Inc()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		metrics.UpstreamErrorsTotal.WithLabelValues("transport_rest", http.MethodGet, compactRoutePath, httpErrorReason(resp.StatusCode)).Inc()
		return nil, 0, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	var decoded compactUpstreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, 0, err
	}

	return transformCompactBoard(id, decoded), decoded.RealtimeDataUpdatedAt, nil
}

// transformCompactBoard is a pure function mapping a decoded upstream response
// to the wire board shape. Kept separate from fetchCompactBoard so it can be
// tested without spinning up an HTTP server.
func transformCompactBoard(id string, upstream compactUpstreamResponse) *compactBoard {
	board := &compactBoard{
		ID:         id,
		Name:       id,
		Departures: []compactDeparture{},
	}

	cutoff := time.Now().Add(-compactPastGrace)
	nameSet := false

	for _, d := range upstream.Departures {
		if !nameSet && d.Stop.Name != "" {
			board.Name = d.Stop.Name
			nameSet = true
		}

		t := effectiveTime(d.When, d.PlannedWhen)
		if t.IsZero() || t.Before(cutoff) {
			continue
		}

		line := "?"
		if d.Line.Name != "" {
			line = d.Line.Name
		}

		delay := 0
		if d.Delay != nil {
			// JavaScript Math.round semantics: half rounds toward +∞.
			delay = int(math.Floor(*d.Delay/60.0 + 0.5))
		}

		cancelled := 0
		if d.Cancelled {
			cancelled = 1
		}

		warning := 0
		for _, rm := range d.Remarks {
			if rm.Type == "warning" {
				warning = 1
				break
			}
		}

		board.Departures = append(board.Departures, compactDeparture{
			Line:      line,
			Product:   d.Line.Product,
			Direction: compactStripDirection(d.Direction),
			Time:      t.Unix(),
			Delay:     delay,
			Cancelled: cancelled,
			Warning:   warning,
		})
	}

	slices.SortStableFunc(board.Departures, func(a, b compactDeparture) int {
		return cmp.Compare(a.Time, b.Time)
	})

	return board
}

// compactStripDirection strips the first matching prefix of "S+U ", "U ", "S "
// (checked in that order, at most one strip), then truncates to
// compactDirectionRunes runes (never bytes — direction names contain umlauts).
func compactStripDirection(dir string) string {
	for _, prefix := range []string{"S+U ", "U ", "S "} {
		if strings.HasPrefix(dir, prefix) {
			dir = strings.TrimPrefix(dir, prefix)
			break
		}
	}

	runes := []rune(dir)
	if len(runes) > compactDirectionRunes {
		runes = runes[:compactDirectionRunes]
	}
	return string(runes)
}

// parseCompactStops validates and parses the "stops" query parameter per §1:
// 1-4 comma-separated segments, each 1-12 ASCII digits; empty segments
// between commas are skipped.
func parseCompactStops(raw string) ([]string, bool) {
	if raw == "" {
		return nil, false
	}

	var stops []string
	for _, seg := range strings.Split(raw, ",") {
		if seg == "" {
			continue
		}
		if !isCompactStopID(seg) {
			return nil, false
		}
		stops = append(stops, seg)
	}

	if len(stops) == 0 || len(stops) > compactMaxStops {
		return nil, false
	}
	return stops, true
}

// isCompactStopID reports whether s matches ^[0-9]{1,12}$.
func isCompactStopID(s string) bool {
	if len(s) == 0 || len(s) > compactMaxIDLen {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseCompactClamped parses raw as an int, falling back to def on empty or
// unparseable input, then clamps the result to [min, max]. Never produces an
// error — duration and limit can never cause a 400.
func parseCompactClamped(raw string, def, min, max int) int {
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// compactCacheKey builds the cache key from canonical, post-validation params.
// limit is included because the marshaled post-limit body is what gets
// cached.
func compactCacheKey(stops []string, duration, limit int) string {
	return compactRoutePath + "?stops=" + strings.Join(stops, ",") +
		"&duration=" + strconv.Itoa(duration) + "&limit=" + strconv.Itoa(limit)
}

// compactCacheExpiry returns the latest departure time across all boards, or
// the zero time if there are no departures at all.
func compactCacheExpiry(boards []compactBoard) time.Time {
	var maxT int64
	for _, b := range boards {
		for _, d := range b.Departures {
			if d.Time > maxT {
				maxT = d.Time
			}
		}
	}
	if maxT == 0 {
		return time.Time{}
	}
	return time.Unix(maxT, 0)
}

// writeCompactJSON writes body with the given status, always setting
// Content-Type: application/json first — the watch hard-fails on any other
// content type.
func writeCompactJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body) //nolint:errcheck
}

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

func transformHAFASCompactBoard(id string, res *hafasStationBoardResult) *compactBoard {
	board := &compactBoard{
		ID:         id,
		Name:       id,
		Departures: []compactDeparture{},
	}

	cutoff := time.Now().Add(-compactPastGrace)
	nameSet := false

	for _, jny := range res.JnyL {
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
			Warning:   0,
		})
	}

	slices.SortStableFunc(board.Departures, func(a, b compactDeparture) int {
		return cmp.Compare(a.Time, b.Time)
	})

	return board
}

func hafasPlanrtTS(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}
