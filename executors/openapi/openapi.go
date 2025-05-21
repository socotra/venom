package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/ovh/venom"
)

// Executor represents an OpenAPI executor
type Executor struct {
	Client     interface{}            `json:"client" yaml:"client"`
	Operation  string                 `json:"operation" yaml:"operation"`
	Parameters map[string]interface{} `json:"parameters" yaml:"parameters"`
	Body       interface{}            `json:"body" yaml:"body"`
	Headers    map[string]string      `json:"headers" yaml:"headers"`
}

// New returns a new Executor
func New() venom.Executor {
	return &Executor{}
}

// Run executes TestStep
func (e *Executor) Run(ctx context.Context, step venom.TestStep) (interface{}, error) {
	// transform step to Executor Instance
	if err := mapstructure.Decode(step, &e); err != nil {
		return nil, err
	}

	if e.Client == nil {
		return nil, fmt.Errorf("client must be provided")
	}

	// Get the client type
	clientType := reflect.TypeOf(e.Client)
	if clientType.Kind() != reflect.Ptr {
		return nil, fmt.Errorf("client must be a pointer to a struct")
	}

	// Find the operation method
	method := reflect.ValueOf(e.Client).MethodByName(e.Operation)
	if !method.IsValid() {
		return nil, fmt.Errorf("operation %s not found in client", e.Operation)
	}

	// Prepare method arguments
	methodType := method.Type()
	numIn := methodType.NumIn()
	args := make([]reflect.Value, numIn)

	// First argument is context
	args[0] = reflect.ValueOf(ctx)

	// Handle remaining arguments based on parameters
	for i := 1; i < numIn; i++ {
		paramType := methodType.In(i)
		paramName := strings.ToLower(paramType.Name())

		// Try to find matching parameter
		if paramValue, ok := e.Parameters[paramName]; ok {
			// Convert parameter to the expected type
			paramValueReflect := reflect.ValueOf(paramValue)
			if paramValueReflect.Type().ConvertibleTo(paramType) {
				args[i] = paramValueReflect.Convert(paramType)
			} else {
				return nil, fmt.Errorf("parameter %s cannot be converted to type %s", paramName, paramType)
			}
		} else if i == numIn-1 && e.Body != nil {
			// Last parameter might be the request body
			bodyValue := reflect.ValueOf(e.Body)
			if bodyValue.Type().ConvertibleTo(paramType) {
				args[i] = bodyValue.Convert(paramType)
			} else {
				return nil, fmt.Errorf("body cannot be converted to type %s", paramType)
			}
		} else {
			// Create zero value for missing parameters
			args[i] = reflect.Zero(paramType)
		}
	}

	// Call the method
	results := method.Call(args)

	// Handle the response
	if len(results) == 0 {
		return nil, fmt.Errorf("operation %s returned no results", e.Operation)
	}

	// First return value is the response
	response := results[0].Interface()

	// Second return value (if exists) is the error
	var err error
	if len(results) > 1 && !results[1].IsNil() {
		err = results[1].Interface().(error)
	}

	if err != nil {
		return nil, err
	}

	// Convert response to map for assertions
	result := make(map[string]interface{})
	if response != nil {
		// Try to convert response to map
		if responseMap, ok := response.(map[string]interface{}); ok {
			result = responseMap
		} else {
			// Convert to JSON and back to get a map
			jsonBytes, err := json.Marshal(response)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response: %v", err)
			}
			if err := json.Unmarshal(jsonBytes, &result); err != nil {
				return nil, fmt.Errorf("failed to unmarshal response: %v", err)
			}
		}
	}

	return result, nil
}

// ZeroValueResult returns an empty instance of the executor's result
func (e *Executor) ZeroValueResult() interface{} {
	return map[string]interface{}{}
}

// GetDefaultAssertions returns default assertions for the executor
func (e *Executor) GetDefaultAssertions() *venom.StepAssertions {
	return &venom.StepAssertions{Assertions: []venom.Assertion{"result.statuscode ShouldEqual 200"}}
}
