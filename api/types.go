package api

import (
	"github.com/heetch/rules-engine/rule"
)

// Response is the response sent to the client after an eval.
type Response struct {
	Data string `json:"data"`
	Type string `json:"type"`
}

// Error is a generic error response.
type Error struct {
	Err string `json:"error"`
}

func (e Error) Error() string {
	return e.Err
}

// Ruleset holds a ruleset and its metadata.
type Ruleset struct {
	Path    string        `json:"path"`
	Ruleset *rule.Ruleset `json:"ruleset"`
}
