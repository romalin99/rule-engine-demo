// Package datasource provides user storage and synthetic data generation for
// the rule engine demo.
package datasource

import (
	"tcg-rulex-engine/pkg/model"
)

// UserStore is an in-memory collection of users with a UID index.
type UserStore struct {
	index map[int64]int
	users []model.User
}

// NewUserStore returns an empty store.
func NewUserStore() *UserStore {
	return &UserStore{index: make(map[int64]int)}
}

// NewUserStoreFrom builds a store from an existing slice.
func NewUserStoreFrom(users []model.User) *UserStore {
	s := NewUserStore()
	s.AddAll(users)
	return s
}

// Add inserts one user.
func (s *UserStore) Add(u model.User) {
	s.index[u.UID] = len(s.users)
	s.users = append(s.users, u)
}

// AddAll inserts many users.
func (s *UserStore) AddAll(users []model.User) {
	for _, u := range users {
		s.Add(u)
	}
}

// Get looks up a user by id.
func (s *UserStore) Get(uid int64) (model.User, bool) {
	i, ok := s.index[uid]
	if !ok {
		return model.User{}, false
	}
	return s.users[i], true
}

// All returns the backing slice (do not mutate).
func (s *UserStore) All() []model.User { return s.users }

// Len reports the number of users.
func (s *UserStore) Len() int { return len(s.users) }
