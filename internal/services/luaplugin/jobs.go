package luaplugin

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	lua "github.com/yuin/gopher-lua"
)

var jobNamePattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// maxJobsPerType bounds colocated job specs in one registration table.
const maxJobsPerType = 8

// Job interval bounds mirror the healthcheck cooldown discipline: frequent
// enough for token refresh, never a busy loop.
const (
	minJobIntervalSeconds = 60
	maxJobIntervalSeconds = 86400
)

// parseJobSpecs validates the colocated jobs table of one registration:
// { name = { interval_seconds, run_on_startup?, timeout_ms?, run } }.
// Bodies are not invoked; schedules persist in the plugin record.
func parseJobSpecs(tbl *lua.LTable, typeKey string) (map[string]JobSpec, error) {
	out := map[string]JobSpec{}
	var names []string
	var firstErr error
	tbl.ForEach(func(k, v lua.LValue) {
		ks, ok := k.(lua.LString)
		if !ok {
			firstErr = fmt.Errorf("plugin type %q: job name must be a string", typeKey)
			return
		}
		names = append(names, string(ks))
	})
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Strings(names)
	if len(names) > maxJobsPerType {
		return nil, fmt.Errorf("plugin type %q: at most %d jobs", typeKey, maxJobsPerType)
	}
	for _, name := range names {
		specTbl, ok := tbl.RawGetString(name).(*lua.LTable)
		if !ok {
			return nil, fmt.Errorf("plugin type %q: job %q must be a table", typeKey, name)
		}
		if !jobNamePattern.MatchString(name) {
			return nil, fmt.Errorf("plugin type %q: invalid job name %q", typeKey, name)
		}
		iv, ok := specTbl.RawGetString("interval_seconds").(lua.LNumber)
		if !ok {
			return nil, fmt.Errorf("plugin type %q: job %q interval_seconds must be a number", typeKey, name)
		}
		interval := int64(iv)
		if interval < minJobIntervalSeconds || interval > maxJobIntervalSeconds {
			return nil, fmt.Errorf("plugin type %q: job %q interval_seconds %d out of range [%d, %d]",
				typeKey, name, interval, minJobIntervalSeconds, maxJobIntervalSeconds)
		}
		runOnStartup := false
		if v := specTbl.RawGetString("run_on_startup"); v != lua.LNil {
			b, ok := v.(lua.LBool)
			if !ok {
				return nil, fmt.Errorf("plugin type %q: job %q run_on_startup must be a boolean", typeKey, name)
			}
			runOnStartup = bool(b)
		}
		timeoutMs := defaultHTTPTimeoutMs
		if v := specTbl.RawGetString("timeout_ms"); v != lua.LNil {
			n, ok := v.(lua.LNumber)
			if !ok {
				return nil, fmt.Errorf("plugin type %q: job %q timeout_ms must be a number", typeKey, name)
			}
			timeoutMs = int(n)
		}
		if timeoutMs < minHTTPTimeoutMs {
			timeoutMs = minHTTPTimeoutMs
		}
		if timeoutMs > maxHTTPTimeoutMs {
			timeoutMs = maxHTTPTimeoutMs
		}
		if fn := specTbl.RawGetString("run"); fn == lua.LNil {
			return nil, fmt.Errorf("plugin type %q: job %q run is required", typeKey, name)
		} else if _, ok := fn.(*lua.LFunction); !ok {
			return nil, fmt.Errorf("plugin type %q: job %q run must be a function", typeKey, name)
		}
		out[name] = JobSpec{IntervalSeconds: interval, RunOnStartup: runOnStartup, TimeoutMs: timeoutMs}
	}
	return out, nil
}

// RunJob invokes the colocated run function of one job spec. Reason is one
// of "tick", "startup" or "manual". The job owns its iteration: list
// credentials, refresh what is stale, persist through credentials.update.
func (s *Service) RunJob(goCtx context.Context, typeKey, name, reason, providerID string, providerConfig map[string]any) error {
	rec, err := s.Lookup(typeKey)
	if err != nil {
		return err
	}
	spec, ok := rec.Jobs[typeKey][name]
	if !ok {
		return &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "job:" + name}
	}
	timeoutMs := spec.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = defaultHTTPTimeoutMs
	}
	ctx, cancel := context.WithTimeout(goCtx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	applyErr := error(nil)
	found, _, err := s.handlerCallEx(ctx, rec, HandlerMeta{TypeKey: typeKey, ProviderID: providerID, ProviderConfig: providerConfig}, "job:"+name, func(L *lua.LState) {
		tbl := L.NewTable()
		tbl.RawSetString("job_name", lua.LString(name))
		tbl.RawSetString("run_reason", lua.LString(reason))
		if len(providerConfig) > 0 {
			tbl.RawSetString("provider_config", toLuaValue(L, providerConfig))
		}
		L.Push(tbl)
	}, 1, func(L *lua.LState) error {
		v := L.Get(-1)
		if v == lua.LNil {
			return nil
		}
		if b, ok := v.(lua.LBool); ok && bool(b) {
			return nil
		}
		if perr, ok := asTerminalError(v); ok {
			applyErr = perr
			return nil
		}
		applyErr = &models.PluginInternalError{PluginID: rec.ID, TypeKey: typeKey, Cause: "job " + name + " must return true or an error table"}
		return applyErr
	}, &callConfig{providerID: providerID, providerConfig: providerConfig, allowCredentialWrite: true, timeoutMs: timeoutMs, jobName: name})
	if err != nil {
		return err
	}
	if !found {
		return &notFoundError{PluginID: rec.ID, TypeKey: typeKey, Handler: "job:" + name}
	}
	return applyErr
}
