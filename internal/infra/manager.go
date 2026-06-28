// Package infra is the central dependency container, mirroring tcg-rulex-engine's
// internal/infra. It holds the rule engine plus optional infrastructure
// (datastore handles, external clients) and the data-access repositories, so the
// application bootstrap (cmd/api) is structured identically to ucs-fe.
//
// Layering: cmd/api → service → infra → repository / engine / clients.
package infra

import (
	"context"

	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"

	"tcg-rulex-engine/internal/client/mcs"
	"tcg-rulex-engine/internal/client/uss"
	"tcg-rulex-engine/internal/client/wps"
	"tcg-rulex-engine/internal/config"
	"tcg-rulex-engine/internal/repository"
	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/logs"
)

// ComClient holds external service clients and datastore handles. The DB layer is
// kept to mirror tcg-rulex-engine; for the in-memory rule engine these are optional and
// may be nil unless the corresponding config is provided (see WireOracle).
type ComClient struct {
	DBX    *sqlx.DB      // Oracle handle (nil unless WireOracle is called)
	Rc     *redis.Client // Redis (nil unless redis.addr configured)
	UssSrv *uss.Client
	McsSrv *mcs.Client
	WpsSrv *wps.Client
}

// ComModel holds all data-access repositories (data layer).
type ComModel struct {
	RuleRepository repository.RuleRepository
}

// ComManager is the central dependency container: the rule engine, the rule
// Manager (hot reload / versioning), infrastructure (clients, datastores) and
// repositories.
type ComManager struct {
	ComClient
	ComModel
	Engine  *engine.Engine  // the rule engine (headline component)
	Manager *engine.Manager // operations: upsert/remove/list/test/publish/rollback
}

// NewComManager builds the rule engine (native front-end + bytecode VM), its
// Manager, and wires optional infrastructure. The Oracle DB is left unwired by
// default so the engine runs in dev/CI without a database — call WireOracle to
// attach it.
func NewComManager(cfg *config.Config) *ComManager {
	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))

	cm := &ComManager{
		Engine:   eng,
		Manager:  engine.NewManager(eng),
		ComModel: ComModel{RuleRepository: repository.NewFileRuleRepository(repository.DefaultRulesPath)},
		ComClient: ComClient{
			UssSrv: cfg.UssSrv.Init(),
			McsSrv: cfg.McsSrv.Init(),
			WpsSrv: cfg.WpsSrv.Init(),
		},
	}

	if len(cfg.Redis.Addr) > 0 {
		cm.Rc = cfg.Redis.Init()
	}

	return cm
}

// NewComManagerWithEngine wraps an already-built engine (no config, no clients).
// Used by the CLI server (router.Serve) and tests, which construct the engine
// themselves (with flag-selected front-end/backend and pre-loaded rules).
func NewComManagerWithEngine(eng *engine.Engine) *ComManager {
	return &ComManager{
		Engine:   eng,
		Manager:  engine.NewManager(eng),
		ComModel: ComModel{RuleRepository: repository.NewFileRuleRepository(repository.DefaultRulesPath)},
	}
}

// WireOracle attaches an Oracle-backed *sqlx.DB and switches the rule repository
// to the DB implementation. Call after credentials are loaded (e.g. from AWS via
// cfg.LoadOracleConnectInfoFromAws). Kept separate so the engine runs without a DB.
func (m *ComManager) WireOracle(cfg *config.Config) {
	m.DBX = cfg.OracleIns.Init()
	m.RuleRepository = repository.NewDBRuleRepository(m.DBX)
}

// Close releases all resources held by the manager (mirrors ucs-fe ordering).
func (m *ComManager) Close() {
	if m.DBX != nil {
		_ = m.DBX.Close()
	}
	if m.UssSrv != nil {
		m.UssSrv.Close()
	}
	if m.McsSrv != nil {
		m.McsSrv.Close()
	}
	if m.WpsSrv != nil {
		m.WpsSrv.Close()
	}
	if m.Rc != nil {
		_ = m.Rc.Close()
	}
	logs.Info(context.Background(), "infra: ComManager closed")
}
