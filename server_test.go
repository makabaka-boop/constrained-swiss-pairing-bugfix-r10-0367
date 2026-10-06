package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postPair(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/pair", bytes.NewReader(buf))
	rec := httptest.NewRecorder()
	pairHandler(rec, req)
	return rec
}

func TestHTTPOK(t *testing.T) {
	rec := postPair(t, PairRequest{Players: players("A", "B", "C", "D")})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "OK" || resp.Result == nil {
		t.Fatalf("unexpected response %s", rec.Body.String())
	}
	if len(resp.Result.Games) != 2 || resp.Result.Bye != "" {
		t.Fatalf("unexpected result %+v", resp.Result)
	}
}

func TestHTTPNoPairing(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White), hist(2, "C", White), hist(3, "D", White)}
	ps[1].History = []Record{hist(1, "A", Black)}
	ps[2].History = []Record{hist(2, "A", Black)}
	ps[3].History = []Record{hist(3, "A", Black)}
	rec := postPair(t, PairRequest{Players: ps})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "NO_PAIRING" || resp.Result != nil {
		t.Fatalf("unexpected response %s", rec.Body.String())
	}
}

func TestHTTPInvalidInput(t *testing.T) {
	// Conflicting colors.
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White)}
	ps[1].History = []Record{hist(1, "A", White)}
	rec := postPair(t, PairRequest{Players: ps})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "INVALID_INPUT" {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
}

func TestHTTPMalformedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/pair", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	pairHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHTTPMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/pair", nil)
	rec := httptest.NewRecorder()
	pairHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
