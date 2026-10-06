package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
)

// errorResponse is returned for malformed or inconsistent requests.
type errorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
}

// pairHandler accepts a JSON PairRequest at POST /pair.
//
// Response codes:
//   - 200 {"status":"OK","result":{...}}            complete optimal schedule;
//   - 200 {"status":"NO_PAIRING"}                   valid input, no full schedule;
//   - 400 {"status":"INVALID_INPUT","error":...}    structurally inconsistent input;
//   - 405                                            non-POST methods.
func pairHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errorResponse{
			Status: "METHOD_NOT_ALLOWED",
			Error:  "use POST",
		})
		return
	}

	var req PairRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Status: "INVALID_INPUT",
			Error:  "cannot decode request: " + err.Error(),
		})
		return
	}

	result, err := Pair(req)
	if err != nil {
		if errors.Is(err, ErrNoPairing) {
			writeJSON(w, http.StatusOK, PairResponse{Status: "NO_PAIRING"})
			return
		}
		if IsValidation(err) {
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Status: "INVALID_INPUT",
				Error:  err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{
			Status: "INTERNAL_ERROR",
			Error:  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, PairResponse{Status: "OK", Result: result})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/pair", pairHandler)
	mux.HandleFunc("/pair/constraints", constraintsHandler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("pairing service listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
