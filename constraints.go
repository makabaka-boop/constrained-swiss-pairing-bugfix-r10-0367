package main

import "encoding/json"
import "net/http"

type scheduleConstraints struct {
	Fixed         []Game      `json:"fixed"`
	Forbidden     [][2]string `json:"forbidden"`
	ByeCandidates []string    `json:"byeCandidates"`
}
type constrainedRequest struct {
	Players     []Player            `json:"players"`
	Constraints scheduleConstraints `json:"constraints"`
}

func constrainedPair(req constrainedRequest) (*PairResult, error) {
	result, err := Pair(PairRequest{Players: req.Players})
	if err != nil {
		return nil, err
	}
	for _, blocked := range req.Constraints.Forbidden {
		for _, g := range result.Games {
			if g.White == blocked[0] && g.Black == blocked[1] || g.White == blocked[1] && g.Black == blocked[0] {
				return nil, ErrNoPairing
			}
		}
	}
	for _, fixed := range req.Constraints.Fixed {
		found := false
		for _, g := range result.Games {
			if g == fixed {
				found = true
			}
		}
		if !found {
			return nil, ErrNoPairing
		}
	}
	return result, nil
}
func constraintsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req constrainedRequest
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		writeJSON(w, 400, errorResponse{"INVALID_INPUT", err.Error()})
		return
	}
	result, err := constrainedPair(req)
	if err != nil {
		if IsValidation(err) {
			writeJSON(w, 400, errorResponse{"INVALID_INPUT", err.Error()})
		} else {
			writeJSON(w, 200, PairResponse{Status: "NO_PAIRING"})
		}
		return
	}
	writeJSON(w, 200, PairResponse{Status: "OK", Result: result})
}
