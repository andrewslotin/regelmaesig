package main

import (
	"encoding/json"
	"strconv"
)

type restDeparturesResponse struct {
	Departures            []restDeparture `json:"departures"`
	RealtimeDataUpdatedAt int64           `json:"realtimeDataUpdatedAt"`
}

type restArrivalsResponse struct {
	Arrivals              []restArrival `json:"arrivals"`
	RealtimeDataUpdatedAt int64         `json:"realtimeDataUpdatedAt"`
}

type restDeparture struct {
	TripID      string     `json:"tripId"`
	Stop        restStop   `json:"stop"`
	When        *string    `json:"when"`
	PlannedWhen string     `json:"plannedWhen"`
	Delay       *int       `json:"delay"`
	Platform    *string    `json:"platform"`
	Direction   string     `json:"direction"`
	Line        restLine   `json:"line"`
	Remarks     []struct{} `json:"remarks"`
	Cancelled   bool       `json:"cancelled"`
}

type restArrival struct {
	TripID      string     `json:"tripId"`
	Stop        restStop   `json:"stop"`
	When        *string    `json:"when"`
	PlannedWhen string     `json:"plannedWhen"`
	Delay       *int       `json:"delay"`
	Platform    *string    `json:"platform"`
	Provenance  string     `json:"provenance"`
	Line        restLine   `json:"line"`
	Remarks     []struct{} `json:"remarks"`
	Cancelled   bool       `json:"cancelled"`
}

type restStop struct {
	Type     string          `json:"type"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Location restLocation    `json:"location"`
	Products map[string]bool `json:"products"`
}

type restLocation struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type restLine struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Product string `json:"product"`
}

type restNearbyStop struct {
	restStop
	Distance int `json:"distance"`
}

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

func translateDepartures(res *hafasStationBoardResult) ([]byte, error) {
	deps := make([]restDeparture, 0, len(res.JnyL))

	for _, jny := range res.JnyL {
		if jny.StbStop.LocX < 0 || jny.StbStop.LocX >= len(res.Common.LocL) {
			continue
		}
		if jny.ProdX < 0 || jny.ProdX >= len(res.Common.ProdL) {
			continue
		}

		loc := res.Common.LocL[jny.StbStop.LocX]
		prod := res.Common.ProdL[jny.ProdX]

		tzOffset := loc.TZOffset
		if tzOffset == 0 {
			tzOffset = 120
		}

		plannedWhen := parseHAFASTime(jny.Date, jny.StbStop.DTimeS, tzOffset)
		when := plannedWhen
		if jny.StbStop.DTimeR != "" {
			when = parseHAFASTime(jny.Date, jny.StbStop.DTimeR, tzOffset)
		}

		var delay *int
		if jny.StbStop.DTimeR != "" {
			d := int(when.Sub(plannedWhen).Seconds())
			delay = &d
		}

		plannedWhenStr := plannedWhen.Format("2006-01-02T15:04:05-07:00")

		var whenPtr *string
		if jny.StbStop.DCncl {
			whenPtr = nil
		} else {
			whenStr := when.Format("2006-01-02T15:04:05-07:00")
			whenPtr = &whenStr
		}

		var platform *string
		if jny.StbStop.DPltfS != nil {
			platform = &jny.StbStop.DPltfS.Txt
		}

		lineName := prod.NameS
		if lineName == "" {
			lineName = prod.Name
		}
		product := hafasProductName(prod.Cls)

		deps = append(deps, restDeparture{
			TripID:      jny.JID,
			Stop:        hafasLocationToRestStop(loc),
			When:        whenPtr,
			PlannedWhen: plannedWhenStr,
			Delay:       delay,
			Platform:    platform,
			Direction:   jny.DirTxt,
			Line: restLine{
				Type:    "line",
				ID:      lineName,
				Name:    lineName,
				Mode:    hafasProductMode(product),
				Product: product,
			},
			Remarks:   []struct{}{},
			Cancelled: jny.StbStop.DCncl,
		})
	}

	var realtimeDataUpdatedAt int64
	if res.PlanrtTS != "" {
		realtimeDataUpdatedAt, _ = strconv.ParseInt(res.PlanrtTS, 10, 64)
	}

	return json.Marshal(restDeparturesResponse{
		Departures:            deps,
		RealtimeDataUpdatedAt: realtimeDataUpdatedAt,
	})
}

func translateArrivals(res *hafasStationBoardResult) ([]byte, error) {
	arrs := make([]restArrival, 0, len(res.JnyL))

	for _, jny := range res.JnyL {
		if jny.StbStop.LocX < 0 || jny.StbStop.LocX >= len(res.Common.LocL) {
			continue
		}
		if jny.ProdX < 0 || jny.ProdX >= len(res.Common.ProdL) {
			continue
		}

		loc := res.Common.LocL[jny.StbStop.LocX]
		prod := res.Common.ProdL[jny.ProdX]

		tzOffset := loc.TZOffset
		if tzOffset == 0 {
			tzOffset = 120
		}

		plannedWhen := parseHAFASTime(jny.Date, jny.StbStop.ATimeS, tzOffset)
		when := plannedWhen
		if jny.StbStop.ATimeR != "" {
			when = parseHAFASTime(jny.Date, jny.StbStop.ATimeR, tzOffset)
		}

		var delay *int
		if jny.StbStop.ATimeR != "" {
			d := int(when.Sub(plannedWhen).Seconds())
			delay = &d
		}

		plannedWhenStr := plannedWhen.Format("2006-01-02T15:04:05-07:00")

		var whenPtr *string
		if jny.StbStop.ACncl {
			whenPtr = nil
		} else {
			whenStr := when.Format("2006-01-02T15:04:05-07:00")
			whenPtr = &whenStr
		}

		var platform *string
		if jny.StbStop.APltfS != nil {
			platform = &jny.StbStop.APltfS.Txt
		}

		lineName := prod.NameS
		if lineName == "" {
			lineName = prod.Name
		}
		product := hafasProductName(prod.Cls)

		arrs = append(arrs, restArrival{
			TripID:      jny.JID,
			Stop:        hafasLocationToRestStop(loc),
			When:        whenPtr,
			PlannedWhen: plannedWhenStr,
			Delay:       delay,
			Platform:    platform,
			Provenance:  jny.DirTxt,
			Line: restLine{
				Type:    "line",
				ID:      lineName,
				Name:    lineName,
				Mode:    hafasProductMode(product),
				Product: product,
			},
			Remarks:   []struct{}{},
			Cancelled: jny.StbStop.ACncl,
		})
	}

	var realtimeDataUpdatedAt int64
	if res.PlanrtTS != "" {
		realtimeDataUpdatedAt, _ = strconv.ParseInt(res.PlanrtTS, 10, 64)
	}

	return json.Marshal(restArrivalsResponse{
		Arrivals:              arrs,
		RealtimeDataUpdatedAt: realtimeDataUpdatedAt,
	})
}

func translateLocations(locs []hafasLocation) ([]byte, error) {
	stops := make([]restStop, len(locs))
	for i, loc := range locs {
		stops[i] = hafasLocationToRestStop(loc)
	}
	return json.Marshal(stops)
}

func translateNearbyLocations(locs []hafasLocation) ([]byte, error) {
	stops := make([]restNearbyStop, len(locs))
	for i, loc := range locs {
		stops[i] = restNearbyStop{
			restStop: hafasLocationToRestStop(loc),
			Distance: loc.Dist,
		}
	}
	return json.Marshal(stops)
}

func translateStop(loc hafasLocation) ([]byte, error) {
	return json.Marshal(hafasLocationToRestStop(loc))
}
