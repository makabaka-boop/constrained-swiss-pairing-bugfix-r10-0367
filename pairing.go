package main

// Pair validates the request and computes the optimal complete schedule
// for the next round. It returns ErrNoPairing when the request is
// structurally valid but no complete legal pairing exists; a partial
// schedule is never produced. Structural inconsistencies yield a
// *ValidationError.
func Pair(req PairRequest) (*PairResult, error) {
	players, _, err := validate(req)
	if err != nil {
		return nil, err
	}
	return newSolver(players).solve()
}
