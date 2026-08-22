package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type HAFASClient struct {
	httpClient *http.Client
	endpoint   string
	authAID    string
	version    string
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

type hafasResponse struct {
	Err     string           `json:"err"`
	SvcResL []hafasSvcResult `json:"svcResL"`
}

type hafasSvcResult struct {
	Meth string          `json:"meth"`
	Err  string          `json:"err"`
	Res  json.RawMessage `json:"res"`
}

type hafasStationBoardResult struct {
	Common   hafasCommon    `json:"common"`
	Type     string         `json:"type"`
	JnyL     []hafasJourney `json:"jnyL"`
	PlanrtTS string         `json:"planrtTS"`
	SD       string         `json:"sD"`
	ST       string         `json:"sT"`
}

type hafasCommon struct {
	LocL  []hafasLocation `json:"locL"`
	ProdL []hafasProduct  `json:"prodL"`
	RemL  []hafasRemark   `json:"remL"`
}

type hafasLocation struct {
	Name     string   `json:"name"`
	ExtID    string   `json:"extId"`
	Crd      hafasCrd `json:"crd"`
	PCls     int      `json:"pCls"`
	Type     string   `json:"type"`
	TZOffset int      `json:"TZOffset"`
	Dist     int      `json:"dist"`
}

type hafasCrd struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type hafasProduct struct {
	Name    string       `json:"name"`
	NameS   string       `json:"nameS"`
	Cls     int          `json:"cls"`
	ProdCtx hafasProdCtx `json:"prodCtx"`
}

type hafasProdCtx struct {
	CatCode string `json:"catCode"`
	CatOut  string `json:"catOut"`
}

type hafasRemark struct {
	Type string `json:"type"`
	Code string `json:"code"`
	TxtN string `json:"txtN"`
}

type hafasJourney struct {
	JID     string       `json:"jid"`
	Date    string       `json:"date"`
	ProdX   int          `json:"prodX"`
	DirTxt  string       `json:"dirTxt"`
	Status  string       `json:"status"`
	StbStop hafasStbStop `json:"stbStop"`
}

type hafasStbStop struct {
	LocX   int            `json:"locX"`
	DProdX int            `json:"dProdX"`
	AProdX int            `json:"aProdX"`
	DTimeS string         `json:"dTimeS"`
	DTimeR string         `json:"dTimeR"`
	DPltfS *hafasPlatform `json:"dPltfS"`
	DCncl  bool           `json:"dCncl"`
	ATimeS string         `json:"aTimeS"`
	ATimeR string         `json:"aTimeR"`
	APltfS *hafasPlatform `json:"aPltfS"`
	ACncl  bool           `json:"aCncl"`
}

type hafasPlatform struct {
	Txt string `json:"txt"`
}

type hafasLocMatchResult struct {
	Match struct {
		LocL []hafasLocation `json:"locL"`
	} `json:"match"`
}

type hafasLocGeoPosResult struct {
	Common hafasCommon     `json:"common"`
	LocL   []hafasLocation `json:"locL"`
}

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
	defer resp.Body.Close() //nolint:errcheck

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

func (c *HAFASClient) stationBoard(ctx context.Context, stopID string, boardType string, duration int) (*hafasStationBoardResult, error) {
	svc, err := c.do(ctx, "StationBoard", map[string]interface{}{
		"type":   boardType,
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

func parseHAFASTime(date, timeStr string, tzOffset int) time.Time {
	if len(date) != 8 || len(timeStr) < 6 {
		return time.Time{}
	}
	year, _ := strconv.Atoi(date[0:4])
	month, _ := strconv.Atoi(date[4:6])
	day, _ := strconv.Atoi(date[6:8])

	var dayOffset, hour int
	if len(timeStr) > 6 {
		dayOffset, _ = strconv.Atoi(timeStr[0 : len(timeStr)-6])
	}
	hour, _ = strconv.Atoi(timeStr[len(timeStr)-6 : len(timeStr)-4])
	min, _ := strconv.Atoi(timeStr[len(timeStr)-4 : len(timeStr)-2])
	sec, _ := strconv.Atoi(timeStr[len(timeStr)-2:])

	loc := time.FixedZone("", tzOffset*60)
	t := time.Date(year, time.Month(month), day+dayOffset, hour, min, sec, 0, loc)
	return t
}

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

func hafasProductsMap(pCls int) map[string]bool {
	return map[string]bool{
		"suburban": pCls&1 != 0,
		"subway":   pCls&2 != 0,
		"tram":     pCls&4 != 0,
		"bus":      pCls&8 != 0,
		"ferry":    pCls&16 != 0,
		"express":  pCls&32 != 0,
		"regional": pCls&64 != 0,
	}
}

func (c *HAFASClient) Departures(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
	path := routePath(r)
	stopID := r.PathValue("id")
	if stopID == "" {
		return nil, nil, fmt.Errorf("missing stop ID")
	}
	duration := 10
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

func (c *HAFASClient) Arrivals(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
	path := routePath(r)
	stopID := r.PathValue("id")
	if stopID == "" {
		return nil, nil, fmt.Errorf("missing stop ID")
	}
	duration := 10
	if d := r.URL.Query().Get("duration"); d != "" {
		if v, err := strconv.Atoi(d); err == nil {
			duration = v
		}
	}
	start := time.Now()
	res, err := c.stationBoard(ctx, stopID, "ARR", duration)
	if err != nil {
		metrics.UpstreamErrorsTotal.WithLabelValues("hafas", r.Method, path, errorReason(err)).Inc()
		return nil, nil, err
	}
	metrics.UpstreamRequestDuration.WithLabelValues("hafas", r.Method, path).Observe(time.Since(start).Seconds())
	metrics.UpstreamRequestsTotal.WithLabelValues("hafas", r.Method, path, "200").Inc()
	body, err := translateArrivals(res)
	if err != nil {
		return nil, nil, err
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return body, header, nil
}

func (c *HAFASClient) Locations(ctx context.Context, r *http.Request, metrics *Metrics) ([]byte, http.Header, error) {
	path := routePath(r)
	query := r.URL.Query().Get("query")
	if query == "" {
		return nil, nil, fmt.Errorf("missing query parameter")
	}
	results := 5
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
	distance := 1000
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
