package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrRegression = errors.New("evals: change regressed")

// Answerer executes the pinned candidate for one input. A replay adapter may
// supply recorded tool results; the gate itself never calls a judging model.
type Answerer func(context.Context, Case) (string, error)

type ChangeScore struct {
	SuiteHash string   `json:"suite_hash"`
	Suite     string   `json:"suite"`
	Before    float64  `json:"before"`
	After     float64  `json:"after"`
	Passed    bool     `json:"passed"`
	Results   []Result `json:"results"`
}

// CheckChange compares identical bounded cases, before the candidate is saved.
// It never treats an interrupted or unexecutable case as a passing result.
func CheckChange(ctx context.Context, suite Suite, before, after Answerer) (ChangeScore, error) {
	out := ChangeScore{Suite: suite.Name, SuiteHash: SuiteHash(suite), Results: []Result{}}
	if err := suite.Validate(); err != nil {
		return out, err
	}
	if suite.Target != Agent || before == nil || after == nil {
		return out, ErrInvalid
	}
	beforePass, afterPass := 0, 0
	for _, c := range suite.Cases {
		for i, answer := range []Answerer{before, after} {
			start := time.Now()
			text, err := answer(ctx, c)
			if err != nil {
				return out, fmt.Errorf("%w: suite %s case %s could not run: %v", ErrRegression, suite.Name, c.ID, err)
			}
			result, err := ScoreAnswer(ctx, c, text, time.Since(start))
			if err != nil {
				return out, err
			}
			if i == 0 {
				if result.Passed {
					beforePass++
				}
			} else {
				if result.Passed {
					afterPass++
				}
				out.Results = append(out.Results, result)
			}
		}
	}
	out.Before = 100 * float64(beforePass) / float64(len(suite.Cases))
	out.After = 100 * float64(afterPass) / float64(len(suite.Cases))
	out.Passed = out.After >= out.Before && out.After >= suite.Threshold()
	if !out.Passed {
		return out, fmt.Errorf("%w: %s scored %.0f%% (was %.0f%%, requires %.0f%%)", ErrRegression, suite.Name, out.After, out.Before, suite.Threshold())
	}
	return out, nil
}

func SuiteHash(s Suite) string { raw, _ := json.Marshal(s); return identity(string(raw)) }
