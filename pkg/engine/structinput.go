package engine

import (
	"encoding/json"
	"fmt"

	"tcg-rulex-engine/pkg/model"
)

// UserFromStruct converts any Go struct (or map) into a wide-table model.User by
// round-tripping through JSON, so the struct's json tags map straight onto
// User.Fields. This lets callers feed typed records — e.g. a DB row or a Kafka
// message struct — without hand-building map[string]any.
//
//	type Profile struct {
//	    UID      int64   `json:"uid"`
//	    Age      int     `json:"age"`
//	    Province string  `json:"province"`
//	    Score    float64 `json:"active_score"`
//	}
//	u, _ := engine.UserFromStruct(Profile{UID: 1, Age: 30, Province: "广东", Score: 96})
func UserFromStruct(v any) (model.User, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return model.User{}, fmt.Errorf("struct input: marshal: %w", err)
	}
	var u model.User
	if err := json.Unmarshal(b, &u); err != nil {
		return model.User{}, fmt.Errorf("struct input: unmarshal: %w", err)
	}
	return u, nil
}

// UsersFromStructs converts a slice of typed records into wide-table users.
func UsersFromStructs[T any](items []T) ([]model.User, error) {
	out := make([]model.User, 0, len(items))
	for i, it := range items {
		u, err := UserFromStruct(it)
		if err != nil {
			return nil, fmt.Errorf("struct input [%d]: %w", i, err)
		}
		out = append(out, u)
	}
	return out, nil
}
