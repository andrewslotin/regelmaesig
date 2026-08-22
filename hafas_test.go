package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseHAFASTime(t *testing.T) {
	t.Run("normal time", func(t *testing.T) {
		got := parseHAFASTime("20260822", "152300", 120)
		want := time.Date(2026, 8, 22, 15, 23, 0, 0, time.FixedZone("", 120*60))
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("midnight rollover", func(t *testing.T) {
		got := parseHAFASTime("20260822", "250000", 120)
		want := time.Date(2026, 8, 23, 1, 0, 0, 0, time.FixedZone("", 120*60))
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("short date returns zero", func(t *testing.T) {
		got := parseHAFASTime("2026", "152300", 120)
		if !got.IsZero() {
			t.Errorf("expected zero time, got %v", got)
		}
	})

	t.Run("short time returns zero", func(t *testing.T) {
		got := parseHAFASTime("20260822", "1523", 120)
		if !got.IsZero() {
			t.Errorf("expected zero time, got %v", got)
		}
	})
}

func TestHAFASProductName(t *testing.T) {
	tests := []struct {
		cls  int
		want string
	}{
		{1, "suburban"},
		{2, "subway"},
		{4, "tram"},
		{8, "bus"},
		{16, "ferry"},
		{32, "express"},
		{64, "regional"},
		{0, ""},
	}
	for _, tt := range tests {
		got := hafasProductName(tt.cls)
		if got != tt.want {
			t.Errorf("hafasProductName(%d) = %q, want %q", tt.cls, got, tt.want)
		}
	}
}

func TestHAFASProductsMap(t *testing.T) {
	m := hafasProductsMap(67) // 1+2+64
	if !m["suburban"] {
		t.Error("expected suburban=true")
	}
	if !m["subway"] {
		t.Error("expected subway=true")
	}
	if !m["regional"] {
		t.Error("expected regional=true")
	}
	if m["tram"] {
		t.Error("expected tram=false")
	}
	if m["bus"] {
		t.Error("expected bus=false")
	}
	if m["ferry"] {
		t.Error("expected ferry=false")
	}
	if m["express"] {
		t.Error("expected express=false")
	}
}

func TestHAFASClientDo(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(hafasResponse{ //nolint:errcheck
				Err: "OK",
				SvcResL: []hafasSvcResult{
					{Meth: "StationBoard", Err: "OK", Res: json.RawMessage(`{"type":"DEP"}`)},
				},
			})
		}))
		defer srv.Close()

		c := NewHAFASClient(srv.Client(), srv.URL, "test-aid", "1.45")
		result, err := c.do(context.Background(), "StationBoard", map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		if result.Meth != "StationBoard" {
			t.Errorf("got method %q, want StationBoard", result.Meth)
		}
	})

	t.Run("top-level error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(hafasResponse{ //nolint:errcheck
				Err: "FAIL",
			})
		}))
		defer srv.Close()

		c := NewHAFASClient(srv.Client(), srv.URL, "test-aid", "1.45")
		_, err := c.do(context.Background(), "StationBoard", map[string]string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("service error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(hafasResponse{ //nolint:errcheck
				Err: "OK",
				SvcResL: []hafasSvcResult{
					{Meth: "StationBoard", Err: "LOCATION", Res: json.RawMessage(`{}`)},
				},
			})
		}))
		defer srv.Close()

		c := NewHAFASClient(srv.Client(), srv.URL, "test-aid", "1.45")
		_, err := c.do(context.Background(), "StationBoard", map[string]string{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
