package engine

type Engine interface {
    LoadRules() error
    Match(map[string]any) ([]int64,error)
}
