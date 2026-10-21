package server

import (
	"log/slog"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/adapters/generic"
	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/services/batches"
	"github.com/TheSlopMachine/llm-router/internal/services/credential"
	"github.com/TheSlopMachine/llm-router/internal/services/doctor"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/modelinfo"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
	"github.com/TheSlopMachine/llm-router/internal/services/responses"
	"github.com/TheSlopMachine/llm-router/internal/services/router"
	"github.com/TheSlopMachine/llm-router/internal/services/token"
	"github.com/TheSlopMachine/llm-router/internal/services/videojobs"
	"github.com/TheSlopMachine/llm-router/internal/services/virtual"
	virtualadapter "github.com/TheSlopMachine/llm-router/providers/virtual"
)

// NewDoctor builds the database doctor without starting a server: no
// listener, no background job, no startup cleanup. The backend wiring
// mirrors New, because the missing-backend check depends on every provider
// adapter being registered; a missing adapter would flag its providers and
// a fix would disable them.
func NewDoctor(sqliteDatabase *db.Database, logger *slog.Logger) (*doctor.GormService, error) {
	luaService, err := luaplugin.New(sqliteDatabase, logger)
	if err != nil {
		return nil, err
	}
	providerService := provider.NewGorm(sqliteDatabase)
	providerService.SetLogger(logger)
	providerService.SetLuaService(luaService)
	genericAdapter := &generic.Adapter{}
	genericAdapter.SetLogger(logger)
	providerService.RegisterGoAdapter(genericAdapter)

	tokenService := token.NewGorm(sqliteDatabase)
	tokenService.SetLogger(logger)
	credentialService := credential.NewGorm(sqliteDatabase, providerService)
	credentialService.SetLogger(logger)
	modelInfoService := modelinfo.NewGorm(sqliteDatabase, providerService, credentialService, 1*time.Hour)
	virtualService := virtual.NewGorm(sqliteDatabase, providerService, modelInfoService)

	routerService := router.New(providerService, credentialService, modelInfoService,
		videojobs.New(sqliteDatabase), responses.NewGormService(sqliteDatabase.Cache),
		batches.NewSQLiteService(sqliteDatabase), logger)
	providerService.RegisterGoAdapter(virtualadapter.New(routerService, virtualService, logger))

	doctorService := doctor.NewGorm(sqliteDatabase, providerService, virtualService, tokenService, providerService, luaService)
	doctorService.SetLogger(logger)
	return doctorService, nil
}
