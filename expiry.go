package main

import (
	"encoding/json"
	"time"
)

// parseTimestamp parses an RFC3339 timestamp, returning zero on failure.
func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// effectiveTime returns primary if parseable, otherwise fallback.
// Used to handle cancelled trips where "when" is null but "plannedWhen" is set.
func effectiveTime(primary, fallback string) time.Time {
	if t := parseTimestamp(primary); !t.IsZero() {
		return t
	}
	return parseTimestamp(fallback)
}

// latestTime returns the latest non-zero time from times, or zero if none.
func latestTime(times ...time.Time) time.Time {
	var latest time.Time
	for _, t := range times {
		if t.After(latest) {
			latest = t
		}
	}
	return latest
}

// -- per-route expiry functions --
// Return zero to signal "do not cache" (empty or unparseable response).

type whenItem struct {
	When        string `json:"when"`
	PlannedWhen string `json:"plannedWhen"`
}

type leg struct {
	Arrival        string `json:"arrival"`
	PlannedArrival string `json:"plannedArrival"`
}

type journey struct {
	Legs []leg `json:"legs"`
}

type stopover struct {
	Arrival        string `json:"arrival"`
	PlannedArrival string `json:"plannedArrival"`
}

type trip struct {
	Stopovers []stopover `json:"stopovers"`
}

func departuresExpiry(body []byte) time.Time {
	var resp struct {
		Departures []whenItem `json:"departures"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Departures) == 0 {
		return time.Time{}
	}
	var times []time.Time
	for _, d := range resp.Departures {
		times = append(times, effectiveTime(d.When, d.PlannedWhen))
	}
	return latestTime(times...)
}

func arrivalsExpiry(body []byte) time.Time {
	var resp struct {
		Arrivals []whenItem `json:"arrivals"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Arrivals) == 0 {
		return time.Time{}
	}
	var times []time.Time
	for _, a := range resp.Arrivals {
		times = append(times, effectiveTime(a.When, a.PlannedWhen))
	}
	return latestTime(times...)
}

func journeysExpiry(body []byte) time.Time {
	var resp struct {
		Journeys []journey `json:"journeys"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Journeys) == 0 {
		return time.Time{}
	}
	var times []time.Time
	for _, j := range resp.Journeys {
		for _, l := range j.Legs {
			times = append(times, effectiveTime(l.Arrival, l.PlannedArrival))
		}
	}
	return latestTime(times...)
}

func refreshJourneyExpiry(body []byte) time.Time {
	var resp struct {
		Journey journey `json:"journey"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Journey.Legs) == 0 {
		return time.Time{}
	}
	var times []time.Time
	for _, l := range resp.Journey.Legs {
		times = append(times, effectiveTime(l.Arrival, l.PlannedArrival))
	}
	return latestTime(times...)
}

func tripsExpiry(body []byte) time.Time {
	var resp struct {
		Trips []trip `json:"trips"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Trips) == 0 {
		return time.Time{}
	}
	var times []time.Time
	for _, tr := range resp.Trips {
		for _, s := range tr.Stopovers {
			times = append(times, effectiveTime(s.Arrival, s.PlannedArrival))
		}
	}
	return latestTime(times...)
}

func tripExpiry(body []byte) time.Time {
	var resp struct {
		Trip trip `json:"trip"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Trip.Stopovers) == 0 {
		return time.Time{}
	}
	var times []time.Time
	for _, s := range resp.Trip.Stopovers {
		times = append(times, effectiveTime(s.Arrival, s.PlannedArrival))
	}
	return latestTime(times...)
}

func reachableFromExpiry(body []byte) time.Time {
	var resp struct {
		Reachable []whenItem `json:"reachable"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || len(resp.Reachable) == 0 {
		return time.Time{}
	}
	var times []time.Time
	for _, r := range resp.Reachable {
		times = append(times, effectiveTime(r.When, r.PlannedWhen))
	}
	return latestTime(times...)
}
