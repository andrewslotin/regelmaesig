package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTranslateDepartures(t *testing.T) {
	t.Run("with realtime and without", func(t *testing.T) {
		res := &hafasStationBoardResult{
			Common: hafasCommon{
				LocL: []hafasLocation{
					{Name: "S+U Alexanderplatz", ExtID: "900100003", Crd: hafasCrd{X: 13411267, Y: 52521508}, PCls: 11, TZOffset: 120},
				},
				ProdL: []hafasProduct{
					{Name: "Bus 200", NameS: "200", Cls: 8, ProdCtx: hafasProdCtx{CatCode: "3", CatOut: "Bus"}},
					{Name: "S7", NameS: "S7", Cls: 1, ProdCtx: hafasProdCtx{CatCode: "0", CatOut: "S"}},
				},
			},
			JnyL: []hafasJourney{
				{
					JID: "1|41638|1|86|22082026", Date: "20260822", ProdX: 0, DirTxt: "S+U Zoologischer Garten",
					StbStop: hafasStbStop{LocX: 0, DTimeS: "232000", DTimeR: "232300", DPltfS: &hafasPlatform{Txt: "Pos. 1"}},
				},
				{
					JID: "1|12345|1|86|22082026", Date: "20260822", ProdX: 1, DirTxt: "Potsdam Hbf",
					StbStop: hafasStbStop{LocX: 0, DTimeS: "233000"},
				},
			},
			PlanrtTS: "1787433776",
		}

		body, err := translateDepartures(res)
		if err != nil {
			t.Fatal(err)
		}

		var result restDeparturesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}

		if len(result.Departures) != 2 {
			t.Fatalf("expected 2 departures, got %d", len(result.Departures))
		}

		// First departure: has realtime
		dep1 := result.Departures[0]
		if dep1.TripID != "1|41638|1|86|22082026" {
			t.Errorf("tripId = %q", dep1.TripID)
		}
		if dep1.When == nil {
			t.Fatal("when should not be nil")
		}
		if dep1.Delay == nil {
			t.Fatal("delay should not be nil")
		}
		if *dep1.Delay != 180 {
			t.Errorf("delay = %d, want 180", *dep1.Delay)
		}
		if dep1.Line.Product != "bus" {
			t.Errorf("product = %q, want bus", dep1.Line.Product)
		}
		if dep1.Line.Name != "200" {
			t.Errorf("line name = %q, want 200", dep1.Line.Name)
		}
		if dep1.Platform == nil || *dep1.Platform != "Pos. 1" {
			t.Errorf("platform = %v", dep1.Platform)
		}
		if dep1.Direction != "S+U Zoologischer Garten" {
			t.Errorf("direction = %q", dep1.Direction)
		}
		if dep1.Stop.ID != "900100003" {
			t.Errorf("stop id = %q", dep1.Stop.ID)
		}

		// Second departure: no realtime
		dep2 := result.Departures[1]
		if dep2.Delay != nil {
			t.Errorf("delay should be nil, got %v", dep2.Delay)
		}
		if dep2.Line.Product != "suburban" {
			t.Errorf("product = %q, want suburban", dep2.Line.Product)
		}

		if result.RealtimeDataUpdatedAt != 1787433776 {
			t.Errorf("realtimeDataUpdatedAt = %d", result.RealtimeDataUpdatedAt)
		}
	})

	t.Run("cancelled departure", func(t *testing.T) {
		res := &hafasStationBoardResult{
			Common: hafasCommon{
				LocL:  []hafasLocation{{Name: "Test", ExtID: "123", Crd: hafasCrd{X: 13000000, Y: 52000000}, PCls: 1, TZOffset: 120}},
				ProdL: []hafasProduct{{Name: "S1", NameS: "S1", Cls: 1}},
			},
			JnyL: []hafasJourney{
				{JID: "test-jid", Date: "20260822", ProdX: 0, DirTxt: "Somewhere",
					StbStop: hafasStbStop{LocX: 0, DTimeS: "120000", DCncl: true}},
			},
		}

		body, err := translateDepartures(res)
		if err != nil {
			t.Fatal(err)
		}

		var result restDeparturesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}

		if len(result.Departures) != 1 {
			t.Fatalf("expected 1 departure, got %d", len(result.Departures))
		}
		if result.Departures[0].When != nil {
			t.Error("when should be nil for cancelled")
		}
		if !result.Departures[0].Cancelled {
			t.Error("cancelled should be true")
		}
	})

	t.Run("midnight rollover time", func(t *testing.T) {
		res := &hafasStationBoardResult{
			Common: hafasCommon{
				LocL:  []hafasLocation{{Name: "Test", ExtID: "123", Crd: hafasCrd{X: 13000000, Y: 52000000}, PCls: 1, TZOffset: 120}},
				ProdL: []hafasProduct{{Name: "S1", NameS: "S1", Cls: 1}},
			},
			JnyL: []hafasJourney{
				{JID: "test-jid", Date: "20260822", ProdX: 0, DirTxt: "Somewhere",
					StbStop: hafasStbStop{LocX: 0, DTimeS: "250000"}},
			},
		}

		body, err := translateDepartures(res)
		if err != nil {
			t.Fatal(err)
		}

		var result restDeparturesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}

		if len(result.Departures) != 1 {
			t.Fatalf("expected 1 departure, got %d", len(result.Departures))
		}

		// 25:00 on Aug 22 = 01:00 on Aug 23
		loc := time.FixedZone("", 120*60)
		expected := time.Date(2026, 8, 23, 1, 0, 0, 0, loc)
		parsed, err := time.Parse("2006-01-02T15:04:05-07:00", result.Departures[0].PlannedWhen)
		if err != nil {
			t.Fatal(err)
		}
		if !parsed.Equal(expected) {
			t.Errorf("plannedWhen = %v, want %v", parsed, expected)
		}
	})

	t.Run("day offset time format", func(t *testing.T) {
		res := &hafasStationBoardResult{
			Common: hafasCommon{
				LocL:  []hafasLocation{{Name: "Test", ExtID: "123", Crd: hafasCrd{X: 13000000, Y: 52000000}, PCls: 1, TZOffset: 120}},
				ProdL: []hafasProduct{{Name: "S1", NameS: "S1", Cls: 1}},
			},
			JnyL: []hafasJourney{
				{JID: "test-jid", Date: "20260822", ProdX: 0, DirTxt: "Somewhere",
					StbStop: hafasStbStop{LocX: 0, DTimeS: "01010700"}},
			},
		}

		body, err := translateDepartures(res)
		if err != nil {
			t.Fatal(err)
		}

		var result restDeparturesResponse
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}

		if len(result.Departures) != 1 {
			t.Fatalf("expected 1 departure, got %d", len(result.Departures))
		}

		// "01010700" on Aug 22 = day+1, 01:07:00 = Aug 23 01:07:00
		loc := time.FixedZone("", 120*60)
		expected := time.Date(2026, 8, 23, 1, 7, 0, 0, loc)
		parsed, err := time.Parse("2006-01-02T15:04:05-07:00", result.Departures[0].PlannedWhen)
		if err != nil {
			t.Fatal(err)
		}
		if !parsed.Equal(expected) {
			t.Errorf("plannedWhen = %v, want %v", parsed, expected)
		}
	})
}

func TestTranslateArrivals(t *testing.T) {
	res := &hafasStationBoardResult{
		Common: hafasCommon{
			LocL:  []hafasLocation{{Name: "Test Stop", ExtID: "900100003", Crd: hafasCrd{X: 13411267, Y: 52521508}, PCls: 11, TZOffset: 120}},
			ProdL: []hafasProduct{{Name: "Bus 200", NameS: "200", Cls: 8}},
		},
		JnyL: []hafasJourney{
			{JID: "arr-jid", Date: "20260822", ProdX: 0, DirTxt: "Coming From Somewhere",
				StbStop: hafasStbStop{LocX: 0, ATimeS: "140000", ATimeR: "140100"}},
		},
		PlanrtTS: "1787433776",
	}

	body, err := translateArrivals(res)
	if err != nil {
		t.Fatal(err)
	}

	var result restArrivalsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	if len(result.Arrivals) != 1 {
		t.Fatalf("expected 1 arrival, got %d", len(result.Arrivals))
	}

	arr := result.Arrivals[0]
	if arr.Provenance != "Coming From Somewhere" {
		t.Errorf("provenance = %q", arr.Provenance)
	}
	if arr.Delay == nil || *arr.Delay != 60 {
		t.Errorf("delay = %v", arr.Delay)
	}
}

func TestTranslateLocations(t *testing.T) {
	locs := []hafasLocation{
		{Name: "Alexanderplatz", ExtID: "900100003", Crd: hafasCrd{X: 13411267, Y: 52521508}, PCls: 11},
		{Name: "Hauptbahnhof", ExtID: "900003201", Crd: hafasCrd{X: 13369422, Y: 52525592}, PCls: 67},
	}

	body, err := translateLocations(locs)
	if err != nil {
		t.Fatal(err)
	}

	var result []restStop
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(result))
	}

	if result[0].Type != "stop" {
		t.Errorf("type = %q", result[0].Type)
	}
	if result[0].ID != "900100003" {
		t.Errorf("id = %q", result[0].ID)
	}
	if result[0].Location.Latitude != 52.521508 {
		t.Errorf("latitude = %f", result[0].Location.Latitude)
	}
	if result[0].Location.Longitude != 13.411267 {
		t.Errorf("longitude = %f", result[0].Location.Longitude)
	}
}

func TestTranslateNearbyLocations(t *testing.T) {
	locs := []hafasLocation{
		{Name: "Nearby Stop", ExtID: "900100003", Crd: hafasCrd{X: 13411267, Y: 52521508}, PCls: 11, Dist: 250},
	}

	body, err := translateNearbyLocations(locs)
	if err != nil {
		t.Fatal(err)
	}

	var result []json.RawMessage
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}

	var stop struct {
		Distance int `json:"distance"`
	}
	if err := json.Unmarshal(result[0], &stop); err != nil {
		t.Fatal(err)
	}
	if stop.Distance != 250 {
		t.Errorf("distance = %d, want 250", stop.Distance)
	}
}

func TestTranslateStop(t *testing.T) {
	loc := hafasLocation{
		Name: "Alexanderplatz", ExtID: "900100003",
		Crd: hafasCrd{X: 13411267, Y: 52521508}, PCls: 11,
	}

	body, err := translateStop(loc)
	if err != nil {
		t.Fatal(err)
	}

	var result restStop
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}

	if result.Type != "stop" {
		t.Errorf("type = %q", result.Type)
	}
	if result.ID != "900100003" {
		t.Errorf("id = %q", result.ID)
	}
}

func TestTranslateDeparturesExpiryCompatibility(t *testing.T) {
	loc := time.FixedZone("", 120*60)
	futureTime := time.Now().In(loc).Add(30 * time.Minute)
	timeStr := futureTime.Format("150405")
	dateStr := futureTime.Format("20060102")

	res := &hafasStationBoardResult{
		Common: hafasCommon{
			LocL:  []hafasLocation{{Name: "Test", ExtID: "123", Crd: hafasCrd{X: 13000000, Y: 52000000}, PCls: 1, TZOffset: 120}},
			ProdL: []hafasProduct{{Name: "S1", NameS: "S1", Cls: 1}},
		},
		JnyL: []hafasJourney{
			{JID: "test", Date: dateStr, ProdX: 0, DirTxt: "Dir",
				StbStop: hafasStbStop{LocX: 0, DTimeS: timeStr}},
		},
	}

	body, err := translateDepartures(res)
	if err != nil {
		t.Fatal(err)
	}

	expiry := departuresExpiry(body)
	if expiry.IsZero() {
		t.Error("departuresExpiry returned zero time for translated HAFAS departures")
	}
	if !expiry.After(time.Now()) {
		t.Error("expiry should be in the future")
	}
}
