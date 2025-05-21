package socotraapi

import (
	"context"
	"github.com/mitchellh/mapstructure"
	"github.com/ovh/venom"
)

// New returns a new Executor
func New() venom.Executor {
	return &Executor{}
}

// Executor struct
type Executor struct {
}

type Result struct {
	// put in testcase.Systemerr by venom if present
}

// Run executes TestStep
func (Executor) Run(ctx context.Context, step venom.TestStep) (interface{}, error) {
	// transform step to Executor Instance
	var e Executor
	if err := mapstructure.Decode(step, &e); err != nil {
		return nil, err
	}
	// prepare result
	r := Result{}

	return r, nil
}
