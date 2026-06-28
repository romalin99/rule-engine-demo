package model

import "encoding/json"

// User is a wide-table user record as it arrives from Kafka (or any upstream).
//
// Fields holds the columns directly as a map, so rule evaluation needs NO
// reflection and NO per-evaluation struct→map conversion: the engine wraps
// Fields into an evaluation context once per user and reuses it for every rule.
//
//	type User struct {
//	    UID    int64
//	    Fields map[string]any
//	}
type User struct {
	Fields map[string]any
	UID    int64
}

// NewUser builds a user from an id and its fields.
func NewUser(uid int64, fields map[string]any) User {
	return User{UID: uid, Fields: fields}
}

// Get returns a single field value.
func (u User) Get(key string) (any, bool) {
	v, ok := u.Fields[key]
	return v, ok
}

// UnmarshalJSON accepts a flat JSON object (e.g. a Kafka message) and stores all
// keys in Fields. The "uid" key (number) is also surfaced as UID.
func (u *User) UnmarshalJSON(b []byte) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	u.Fields = m
	switch v := m["uid"].(type) {
	case float64:
		u.UID = int64(v)
	case int64:
		u.UID = v
	case int:
		u.UID = int64(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			u.UID = n
		}
	}
	return nil
}

// MarshalJSON writes the raw field map.
func (u User) MarshalJSON() ([]byte, error) {
	if u.Fields == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(u.Fields)
}
