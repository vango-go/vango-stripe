package stripe

import (
	"encoding/json"
	"fmt"
)

// UnmarshalEventData unmarshals the event's data.object into the target type.
func UnmarshalEventData[T any](ctx *EventContext) (*T, error) {
	if ctx == nil {
		return nil, fmt.Errorf("stripe: event context is nil")
	}
	if ctx.Event.Data == nil {
		return nil, fmt.Errorf("stripe: unmarshal %T from event %s: missing data.object", *new(T), ctx.Event.Type)
	}

	var target T
	if err := json.Unmarshal(ctx.Event.Data.Raw, &target); err != nil {
		return nil, fmt.Errorf("stripe: unmarshal %T from event %s: %w", target, ctx.Event.Type, err)
	}
	return &target, nil
}
