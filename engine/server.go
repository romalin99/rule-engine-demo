package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/example/rule-engine-demo/model"
)

// Server exposes the engine over HTTP for real-time, online scoring: send one
// user (or a batch) and get back the rules they matched. The rule cache is
// shared, so requests never re-parse.
type Server struct {
	eng *Engine
}

// NewServer wraps an engine in an HTTP handler.
func NewServer(eng *Engine) *Server { return &Server{eng: eng} }

// matchResponse is returned by /match for a single user.
type matchResponse struct {
	UID     int64    `json:"uid"`
	Hits    int      `json:"hits"`
	RuleIDs []int64  `json:"rule_ids"`
	Names   []string `json:"rule_names"`
	TookMs  float64  `json:"took_ms"`
}

// Routes registers the HTTP handlers on a fresh mux.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/rules", s.handleRules)
	mux.HandleFunc("/match", s.handleMatch)      // single user
	mux.HandleFunc("/match/batch", s.handleBatch) // array of users
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSONResp(w, http.StatusOK, map[string]any{"ok": true, "rules": s.eng.RuleCount()})
}

func (s *Server) handleRules(w http.ResponseWriter, _ *http.Request) {
	writeJSONResp(w, http.StatusOK, map[string]any{"rules": s.eng.RuleCount()})
}

func (s *Server) handleMatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST a user JSON body", http.StatusMethodNotAllowed)
		return
	}
	var u model.User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, "bad user json: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSONResp(w, http.StatusOK, s.scoreOne(u))
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST a JSON array of users", http.StatusMethodNotAllowed)
		return
	}
	var users []model.User
	if err := json.NewDecoder(r.Body).Decode(&users); err != nil {
		http.Error(w, "bad users json: "+err.Error(), http.StatusBadRequest)
		return
	}
	out := make([]matchResponse, len(users))
	for i := range users {
		if users[i].UID == 0 {
			users[i].UID = int64(i + 1)
		}
		out[i] = s.scoreOne(users[i])
	}
	writeJSONResp(w, http.StatusOK, out)
}

func (s *Server) scoreOne(u model.User) matchResponse {
	t0 := time.Now()
	ids := s.eng.Match(u)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if p, ok := s.eng.Cache().Get(id); ok {
			names = append(names, p.Name)
		}
	}
	return matchResponse{
		UID:     u.UID,
		Hits:    len(ids),
		RuleIDs: ids,
		Names:   names,
		TookMs:  float64(time.Since(t0).Microseconds()) / 1000.0,
	}
}

func writeJSONResp(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// Serve loads rules into a new engine and serves HTTP until interrupted.
func Serve(addr string, eng *Engine) error {
	srv := NewServer(eng)
	fmt.Printf("rule-engine serving on %s (rules=%d)\n", addr, eng.RuleCount())
	fmt.Println("  POST /match        single user  -> matched rules")
	fmt.Println("  POST /match/batch  []user       -> matched rules")
	fmt.Println("  GET  /rules        rule count")
	return http.ListenAndServe(addr, srv.Routes())
}
