package rules

import (
	"strconv"

	"github.com/heetch/rules-engine/rule"
	"github.com/pkg/errors"
)

var (
	// ErrTypeMismatch is returned when the evaluated rule doesn't return the expected result type.
	ErrTypeMismatch = errors.New("type returned by rule doesn't match")
)

// Engine fetches the rules from the store and executes the selected ruleset.
type Engine struct {
	store Store
}

// NewEngine creates an Engine using the given store.
func NewEngine(store Store) *Engine {
	return &Engine{
		store: store,
	}
}

// Get evaluates the ruleset associated with key and returns the result.
func (e *Engine) get(typ, key string, params rule.Params) (*rule.Result, error) {
	ruleset, err := e.store.Get(key)
	if err != nil {
		if err == ErrRulesetNotFound {
			return nil, err
		}

		return nil, errors.Wrap(err, "failed to get ruleset from the store")
	}

	if ruleset.Type != typ {
		return nil, ErrTypeMismatch
	}

	res, err := ruleset.Eval(params)
	if err != nil {
		if err == rule.ErrNoMatch {
			return nil, err
		}

		return nil, errors.Wrap(err, "failed to evaluate ruleset")
	}

	if res.Type != typ {
		return nil, ErrTypeMismatch
	}

	return res, nil
}

// GetString evaluates the ruleset associated with key and returns the result as a string.
func (e *Engine) GetString(key string, params rule.Params) (string, error) {
	res, err := e.get("string", key, params)
	if err != nil {
		return "", err
	}

	return res.Value, nil
}

// GetBool evaluates the ruleset associated with key and returns the result as a string.
func (e *Engine) GetBool(key string, params rule.Params) (bool, error) {
	res, err := e.get("bool", key, params)
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(res.Value)
}
