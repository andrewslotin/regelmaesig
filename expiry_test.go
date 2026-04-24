package main

import (
	"testing"
	"time"
)

const (
	t1 = "2024-06-01T10:00:00+02:00"
	t2 = "2024-06-01T11:00:00+02:00"
	t3 = "2024-06-01T12:00:00+02:00"
)

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestDeparturesExpiry(t *testing.T) {
	tests := []struct {
		name string
		body string
		want time.Time
	}{
		{
			name: "returns last when",
			body: `{"departures":[{"when":"` + t1 + `"},{"when":"` + t2 + `"},{"when":"` + t3 + `"}]}`,
			want: mustParseTime(t3),
		},
		{
			name: "uses plannedWhen for cancelled trip",
			body: `{"departures":[{"when":null,"plannedWhen":"` + t2 + `"},{"when":"` + t1 + `"}]}`,
			want: mustParseTime(t2),
		},
		{
			name: "empty departures returns zero",
			body: `{"departures":[]}`,
			want: time.Time{},
		},
		{
			name: "invalid JSON returns zero",
			body: `not json`,
			want: time.Time{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := departuresExpiry([]byte(tc.body))
			if !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestArrivalsExpiry(t *testing.T) {
	body := `{"arrivals":[{"when":"` + t1 + `"},{"when":"` + t3 + `"}]}`
	got := arrivalsExpiry([]byte(body))
	if !got.Equal(mustParseTime(t3)) {
		t.Errorf("got %v, want %v", got, mustParseTime(t3))
	}
}

func TestArrivalsExpiry_Empty(t *testing.T) {
	got := arrivalsExpiry([]byte(`{"arrivals":[]}`))
	if !got.IsZero() {
		t.Errorf("expected zero for empty arrivals, got %v", got)
	}
}

func TestJourneysExpiry(t *testing.T) {
	body := `{"journeys":[{"legs":[{"arrival":"` + t1 + `"},{"arrival":"` + t2 + `"}]},{"legs":[{"arrival":"` + t3 + `"}]}]}`
	got := journeysExpiry([]byte(body))
	if !got.Equal(mustParseTime(t3)) {
		t.Errorf("got %v, want %v", got, mustParseTime(t3))
	}
}

func TestJourneysExpiry_UsesPlannedArrival(t *testing.T) {
	body := `{"journeys":[{"legs":[{"arrival":null,"plannedArrival":"` + t2 + `"},{"arrival":"` + t1 + `"}]}]}`
	got := journeysExpiry([]byte(body))
	if !got.Equal(mustParseTime(t2)) {
		t.Errorf("got %v, want %v", got, mustParseTime(t2))
	}
}

func TestJourneysExpiry_Empty(t *testing.T) {
	got := journeysExpiry([]byte(`{"journeys":[]}`))
	if !got.IsZero() {
		t.Errorf("expected zero for empty journeys, got %v", got)
	}
}

func TestRefreshJourneyExpiry(t *testing.T) {
	body := `{"journey":{"legs":[{"arrival":"` + t1 + `"},{"arrival":"` + t3 + `"}]}}`
	got := refreshJourneyExpiry([]byte(body))
	if !got.Equal(mustParseTime(t3)) {
		t.Errorf("got %v, want %v", got, mustParseTime(t3))
	}
}

func TestRefreshJourneyExpiry_Empty(t *testing.T) {
	got := refreshJourneyExpiry([]byte(`{"journey":{"legs":[]}}`))
	if !got.IsZero() {
		t.Errorf("expected zero for empty legs, got %v", got)
	}
}

func TestTripsExpiry(t *testing.T) {
	body := `{"trips":[{"stopovers":[{"arrival":"` + t1 + `"}]},{"stopovers":[{"arrival":"` + t3 + `"}]}]}`
	got := tripsExpiry([]byte(body))
	if !got.Equal(mustParseTime(t3)) {
		t.Errorf("got %v, want %v", got, mustParseTime(t3))
	}
}

func TestTripsExpiry_Empty(t *testing.T) {
	got := tripsExpiry([]byte(`{"trips":[]}`))
	if !got.IsZero() {
		t.Errorf("expected zero for empty trips, got %v", got)
	}
}

func TestTripExpiry(t *testing.T) {
	body := `{"trip":{"stopovers":[{"arrival":"` + t1 + `"},{"arrival":"` + t2 + `"}]}}`
	got := tripExpiry([]byte(body))
	if !got.Equal(mustParseTime(t2)) {
		t.Errorf("got %v, want %v", got, mustParseTime(t2))
	}
}

func TestTripExpiry_UsesPlannedArrival(t *testing.T) {
	body := `{"trip":{"stopovers":[{"arrival":null,"plannedArrival":"` + t2 + `"}]}}`
	got := tripExpiry([]byte(body))
	if !got.Equal(mustParseTime(t2)) {
		t.Errorf("got %v, want %v", got, mustParseTime(t2))
	}
}

func TestTripExpiry_Empty(t *testing.T) {
	got := tripExpiry([]byte(`{"trip":{"stopovers":[]}}`))
	if !got.IsZero() {
		t.Errorf("expected zero for empty stopovers, got %v", got)
	}
}

func TestReachableFromExpiry(t *testing.T) {
	body := `{"reachable":[{"when":"` + t1 + `"},{"when":"` + t3 + `"}]}`
	got := reachableFromExpiry([]byte(body))
	if !got.Equal(mustParseTime(t3)) {
		t.Errorf("got %v, want %v", got, mustParseTime(t3))
	}
}

func TestReachableFromExpiry_Empty(t *testing.T) {
	got := reachableFromExpiry([]byte(`{"reachable":[]}`))
	if !got.IsZero() {
		t.Errorf("expected zero for empty reachable, got %v", got)
	}
}
