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

// Constants for the /garmin/departures endpoint. Prefixed "garmin" to avoid
// collisions with the rest of package main.
const (
	garminRoutePath = "/garmin/departures"
	garminUserAgent = "regelmaesig/1.0 (+https://github.com/andrewslotin/regelmaesig)"

	garminMaxStops       = 4
	garminMaxIDLen       = 12
	garminDirectionRunes = 24
	garminPastGrace      = 30 * time.Second

	garminDefaultDuration = 30
	garminMinDuration     = 10
	garminMaxDuration     = 120

	garminDefaultLimit = 6
	garminMinLimit     = 1
	garminMaxLimit     = 10
)

// Error response bodies, shared with tests.
const (
	garminErrBadRequest = `{"error":"bad_request"}`
	garminErrUpstream   = `{"error":"upstream"}`
	garminErrInternal   = `{"error":"internal"}`
)

// -- wire response schema (field order is significant: encoding/json marshals
// struct fields in declaration order, and tests assert exact bodies) --

type garminResponse struct {
	AsOf    int64         `json:"asOf"`
	Partial int           `json:"partial"`
	Boards  []garminBoard `json:"boards"`
}

type garminBoard struct {
	ID         string            `json:"id"`
	Name       string            `json:"n"`
	Departures []garminDeparture `json:"d"`
}

type garminDeparture struct {
	Line      string `json:"l"`
	Product   string `json:"p"`
	Direction string `json:"dir"`
	Time      int64  `json:"t"`
	Delay     int    `json:"dl"`
	Cancelled int    `json:"c"`
	Warning   int    `json:"w"`
}

// -- upstream decode schema (only the fields this endpoint needs) --

type garminUpstreamResponse struct {
	Departures            []garminUpstreamDeparture `json:"departures"`
	RealtimeDataUpdatedAt int64                     `json:"realtimeDataUpdatedAt"`
}

type garminUpstreamDeparture struct {
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

// handleGarminDepartures serves a compact multi-stop departure board for the
// Garmin watch app. Unlike newStandardHandler/newPassthroughHandler, it fans
// out to N synthesized upstream requests (one per requested stop) and
// transforms the payload; the cache is used purely as a stale-if-error
// fallback when every fetch fails — there is no "serve from cache while
// fresh" fast path.
func handleGarminDepartures(client *http.Client, upstream string, cache *Cache, metrics *Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stops, ok := parseGarminStops(r.URL.Query().Get("stops"))
		if !ok {
			writeGarminJSON(w, http.StatusBadRequest, []byte(garminErrBadRequest))
			return
		}
		duration := parseGarminClamped(r.URL.Query().Get("duration"), garminDefaultDuration, garminMinDuration, garminMaxDuration)
		limit := parseGarminClamped(r.URL.Query().Get("limit"), garminDefaultLimit, garminMinLimit, garminMaxLimit)

		key := garminCacheKey(stops, duration, limit)

		// One goroutine per requested stop, each writing only to its own
		// index of results — no mutex needed since indices never overlap.
		results := make([]garminFetchResult, len(stops))
		var wg sync.WaitGroup
		wg.Add(len(stops))
		for i, id := range stops {
			go func(i int, id string) {
				defer wg.Done()
				board, updatedAt, err := fetchGarminBoard(r.Context(), client, upstream, id, duration, metrics)
				results[i] = garminFetchResult{board: board, realtimeDataUpdatedAt: updatedAt, err: err}
			}(i, id)
		}
		wg.Wait()

		boards := make([]garminBoard, 0, len(stops))
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
				writeGarminJSON(w, http.StatusOK, entry.body)
				return
			}
			metrics.FallbackResponsesTotal.WithLabelValues(http.MethodGet, garminRoutePath).Inc()
			writeGarminJSON(w, http.StatusBadGateway, []byte(garminErrUpstream))
			return
		}

		if asOf == 0 {
			asOf = time.Now().Unix()
		}
		partial := 0
		if failed {
			partial = 1
		}

		body, err := json.Marshal(garminResponse{AsOf: asOf, Partial: partial, Boards: boards})
		if err != nil {
			writeGarminJSON(w, http.StatusInternalServerError, []byte(garminErrInternal))
			return
		}

		// Cache only complete, non-empty responses; expiry is the latest
		// departure time in the body, so a body with no departures at all
		// (expiresAt zero) is never stored either.
		if partial == 0 {
			if expiresAt := garminCacheExpiry(boards); expiresAt.After(time.Now()) {
				cache.Set(key, &cacheEntry{
					statusCode: http.StatusOK,
					body:       body,
					expiresAt:  expiresAt,
				})
			}
		}

		writeGarminJSON(w, http.StatusOK, body)
	}
}

// garminFetchResult carries the outcome of a single per-stop upstream fetch.
type garminFetchResult struct {
	board                 *garminBoard
	realtimeDataUpdatedAt int64
	err                   error
}

// fetchGarminBoard fetches and transforms a single stop's departure board.
func fetchGarminBoard(ctx context.Context, client *http.Client, upstream, id string, duration int, metrics *Metrics) (*garminBoard, int64, error) {
	url := upstream + "/stops/" + id + "/departures?duration=" + strconv.Itoa(duration) +
		"&results=20&remarks=true&language=en&suburban=true&subway=true&tram=true&bus=true&ferry=true&regional=true&express=false&pretty=false"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", garminUserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		metrics.UpstreamErrorsTotal.WithLabelValues(http.MethodGet, garminRoutePath, errorReason(err)).Inc()
		return nil, 0, err
	}
	defer resp.Body.Close() //nolint:errcheck

	metrics.UpstreamRequestDuration.WithLabelValues(http.MethodGet, garminRoutePath).Observe(time.Since(start).Seconds())
	metrics.UpstreamRequestsTotal.WithLabelValues(http.MethodGet, garminRoutePath, strconv.Itoa(resp.StatusCode)).Inc()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		metrics.UpstreamErrorsTotal.WithLabelValues(http.MethodGet, garminRoutePath, httpErrorReason(resp.StatusCode)).Inc()
		return nil, 0, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	var decoded garminUpstreamResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, 0, err
	}

	return transformGarminBoard(id, decoded), decoded.RealtimeDataUpdatedAt, nil
}

// transformGarminBoard is a pure function mapping a decoded upstream response
// to the wire board shape. Kept separate from fetchGarminBoard so it can be
// tested without spinning up an HTTP server.
func transformGarminBoard(id string, upstream garminUpstreamResponse) *garminBoard {
	board := &garminBoard{
		ID:         id,
		Name:       id,
		Departures: []garminDeparture{},
	}

	cutoff := time.Now().Add(-garminPastGrace)
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

		board.Departures = append(board.Departures, garminDeparture{
			Line:      line,
			Product:   d.Line.Product,
			Direction: garminStripDirection(d.Direction),
			Time:      t.Unix(),
			Delay:     delay,
			Cancelled: cancelled,
			Warning:   warning,
		})
	}

	slices.SortStableFunc(board.Departures, func(a, b garminDeparture) int {
		return cmp.Compare(a.Time, b.Time)
	})

	return board
}

// garminStripDirection strips the first matching prefix of "S+U ", "U ", "S "
// (checked in that order, at most one strip), then truncates to
// garminDirectionRunes runes (never bytes — direction names contain umlauts).
func garminStripDirection(dir string) string {
	for _, prefix := range []string{"S+U ", "U ", "S "} {
		if strings.HasPrefix(dir, prefix) {
			dir = strings.TrimPrefix(dir, prefix)
			break
		}
	}

	runes := []rune(dir)
	if len(runes) > garminDirectionRunes {
		runes = runes[:garminDirectionRunes]
	}
	return string(runes)
}

// parseGarminStops validates and parses the "stops" query parameter per §1:
// 1-4 comma-separated segments, each 1-12 ASCII digits; empty segments
// between commas are skipped.
func parseGarminStops(raw string) ([]string, bool) {
	if raw == "" {
		return nil, false
	}

	var stops []string
	for _, seg := range strings.Split(raw, ",") {
		if seg == "" {
			continue
		}
		if !isGarminStopID(seg) {
			return nil, false
		}
		stops = append(stops, seg)
	}

	if len(stops) == 0 || len(stops) > garminMaxStops {
		return nil, false
	}
	return stops, true
}

// isGarminStopID reports whether s matches ^[0-9]{1,12}$.
func isGarminStopID(s string) bool {
	if len(s) == 0 || len(s) > garminMaxIDLen {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// parseGarminClamped parses raw as an int, falling back to def on empty or
// unparseable input, then clamps the result to [min, max]. Never produces an
// error — duration and limit can never cause a 400.
func parseGarminClamped(raw string, def, min, max int) int {
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

// garminCacheKey builds the cache key from canonical, post-validation params.
// limit is included because the marshaled post-limit body is what gets
// cached.
func garminCacheKey(stops []string, duration, limit int) string {
	return garminRoutePath + "?stops=" + strings.Join(stops, ",") +
		"&duration=" + strconv.Itoa(duration) + "&limit=" + strconv.Itoa(limit)
}

// garminCacheExpiry returns the latest departure time across all boards, or
// the zero time if there are no departures at all.
func garminCacheExpiry(boards []garminBoard) time.Time {
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

// writeGarminJSON writes body with the given status, always setting
// Content-Type: application/json first — the watch hard-fails on any other
// content type.
func writeGarminJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body) //nolint:errcheck
}
