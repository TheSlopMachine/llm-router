package luaplugin

import (
	"context"
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

// Health-check cooldown bounds for the healthcheck_cooldown registration
// value. Absent means DefaultHealthCooldown.
const (
	DefaultHealthCooldown = 5 * time.Minute
	MinHealthCooldown     = time.Minute
	MaxHealthCooldown     = 24 * time.Hour
)

// HealthStatus is the verdict of a check_health handler call.
type HealthStatus string

// Health verdicts: healthy keeps the credential, unhealthy disables it,
// unknown (and any handler failure) changes nothing.
const (
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthUnknown   HealthStatus = "unknown"
)

// parseHealthCooldown validates one registration's healthcheck_cooldown
// value: a whole number of seconds within [MinHealthCooldown,
// MaxHealthCooldown]. Out-of-range and mistyped values reject the plugin
// at install: a typo'd cooldown must never deploy as a silent default.
func parseHealthCooldown(v lua.LValue, typeKey string) (time.Duration, error) {
	n, ok := v.(lua.LNumber)
	if !ok {
		return 0, fmt.Errorf("plugin type %q: healthcheck_cooldown must be a number of seconds", typeKey)
	}
	if float64(n) != float64(int64(n)) {
		return 0, fmt.Errorf("plugin type %q: healthcheck_cooldown must be whole seconds", typeKey)
	}
	d := time.Duration(int64(n)) * time.Second
	if d < MinHealthCooldown || d > MaxHealthCooldown {
		return 0, fmt.Errorf("plugin type %q: healthcheck_cooldown %s out of range [%s, %s]", typeKey, d, MinHealthCooldown, MaxHealthCooldown)
	}
	return d, nil
}

// HealthTrigger receives failed attempt identities for detached
// health-check dispatch. Implemented by the healthcheck service; kept as
// an interface here so the call direction never cycles.
type HealthTrigger interface {
	SuspectFailed(pluginID, typeKey, credentialID string)
}

// HealthCooldown resolves the health-check cooldown for one type key:
// the per-type registration value when present, DefaultHealthCooldown
// otherwise. Unknown types fall back to the default: a struggling store
// never blocks traffic.
func (s *Service) HealthCooldown(typeKey string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if rec, ok := s.registry[typeKey]; ok {
		if secs, ok := rec.HealthCooldown[typeKey]; ok {
			return time.Duration(secs) * time.Second
		}
	}
	return DefaultHealthCooldown
}

// CheckHealth invokes the check_health handler for one credential and
// returns its verdict. Missing handler reports ErrHandlerNotFound (the
// type never health-checks). Invalid verdict shapes become
// PluginInternalError and change nothing: only an explicit "unhealthy"
// may disable.
func (s *Service) CheckHealth(goCtx context.Context, typeKey string, cred *models.Credential) (HealthStatus, string, error) {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return "", "", err
	}
	var status HealthStatus
	var message string
	found, err := s.handlerCall(goCtx, rec, typeKey, string(HandlerCheckHealth), func(L *lua.LState) {
		L.Push(ctxTable(L, "", nil))
		L.Push(credTable(L, cred))
	}, 1, func(L *lua.LState) error {
		tbl, ok := L.Get(-1).(*lua.LTable)
		if !ok {
			s.recordCrash(rec.ID, typeKey, "check_health must return a table")
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "check_health must return a table"}
		}
		raw, ok := tbl.RawGetString("status").(lua.LString)
		if !ok {
			s.recordCrash(rec.ID, typeKey, "check_health table needs a status string")
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "check_health table needs a status string"}
		}
		switch HealthStatus(raw) {
		case HealthHealthy, HealthUnhealthy, HealthUnknown:
			status = HealthStatus(raw)
		default:
			s.recordCrash(rec.ID, typeKey, "check_health has unknown status")
			return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: fmt.Sprintf("check_health has unknown status %q", string(raw))}
		}
		if mv := tbl.RawGetString("message"); mv != lua.LNil {
			ms, ok := mv.(lua.LString)
			if !ok {
				s.recordCrash(rec.ID, typeKey, "check_health message must be a string")
				return &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "check_health message must be a string"}
			}
			message = string(ms)
		}
		return nil
	}, nil)
	if err != nil {
		return "", "", err
	}
	if !found {
		return "", "", &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: string(HandlerCheckHealth)}
	}
	return status, message, nil
}
