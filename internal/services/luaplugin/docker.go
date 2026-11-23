package luaplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/dockerhost"
	lua "github.com/yuin/gopher-lua"
)

// Docker call bounds of the Lua surface.
const (
	defaultDockerWaitMs    = 60000
	maximumDockerWaitMs    = 900000
	defaultDockerExecMs    = 60000
	defaultStopSeconds     = 10
	maximumStopSeconds     = 300
	defaultDockerLogTail   = 200
	maximumDockerLogTail   = 10000
	defaultContainerHTTPMs = 60000
)

// dockerBridge binds the Docker service to one plugin policy for one call.
type dockerBridge struct {
	service *dockerhost.Service
	policy  dockerhost.Policy
}

// SetDocker wires the Docker host service for plugins declaring
// @require_api docker. Removing the Docker host disables those plugins.
func (s *Service) SetDocker(service *dockerhost.Service) {
	s.wiringMu.Lock()
	s.docker = service
	s.wiringMu.Unlock()
	service.SetConnectionRemovedHook(s.disableDockerPlugins)
}

func (s *Service) dockerHost() *dockerhost.Service {
	s.wiringMu.RLock()
	defer s.wiringMu.RUnlock()
	return s.docker
}

// dockerConfigured reports whether a Docker host is configured. Without the
// Docker service no host is.
func (s *Service) dockerConfigured(ctx context.Context) (bool, error) {
	dockerService := s.dockerHost()
	if dockerService == nil {
		return false, nil
	}
	connection, err := dockerService.Connection(ctx)
	if err != nil {
		return false, fmt.Errorf("read docker connection: %w", err)
	}
	return connection.Endpoint != "", nil
}

// waitsForDocker reports whether a plugin source must stay disabled: it
// needs Docker and no Docker host is configured.
func (s *Service) waitsForDocker(ctx context.Context, manifest *Manifest) (bool, error) {
	if !manifest.RequiresAPI(dockerhost.API) {
		return false, nil
	}
	configured, err := s.dockerConfigured(ctx)
	return !configured, err
}

// disableDockerPlugins disables every enabled plugin that needs Docker. It
// runs after the Docker host is removed.
func (s *Service) disableDockerPlugins(ctx context.Context) error {
	records, err := s.ListSQLite(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, record := range enabledRecords(records) {
		_, declared, err := s.dockerPolicy(record)
		if err == nil && declared {
			_, err = s.SetDisabled(ctx, record.ID, true)
		}
		failures = errors.Join(failures, err)
	}
	return failures
}

// afterInitialize starts preparing the images of an enabled Docker plugin
// once its hook has run.
func (s *Service) afterInitialize(record *PluginRecord) {
	dockerService := s.dockerHost()
	if dockerService == nil || record.Disabled {
		return
	}
	if policy, declared, err := s.dockerPolicy(record); err == nil && declared {
		dockerService.PrepareImages(policy)
	}
}

// manifestFacts are the manifest values read on every call, memoized per
// source revision.
type manifestFacts struct {
	permissions Permissions
	policy      dockerhost.Policy
	declared    bool
}

func (s *Service) manifestFacts(record *PluginRecord) (manifestFacts, error) {
	key := record.ID + "\x00" + record.Version + "\x00" + record.UpdatedAt.String()
	if cached, ok := s.manifestCache.Load(key); ok {
		return cached.(manifestFacts), nil
	}
	manifest, err := ParseManifest(record.Source)
	if err != nil {
		return manifestFacts{}, fmt.Errorf("parse manifest of %s: %w", record.ID, err)
	}
	policy, declared := manifest.DockerPolicy(record.ID)
	facts := manifestFacts{permissions: manifest.Permissions(), policy: policy, declared: declared}
	s.manifestCache.Store(key, facts)
	return facts, nil
}

// dockerPolicy returns the Docker policy of a record.
func (s *Service) dockerPolicy(record *PluginRecord) (dockerhost.Policy, bool, error) {
	facts, err := s.manifestFacts(record)
	return facts.policy, facts.declared, err
}

// Permissions returns the permissions an installed record requests.
func (s *Service) Permissions(record *PluginRecord) (Permissions, error) {
	facts, err := s.manifestFacts(record)
	return facts.permissions, err
}

// DockerPolicies lists the Docker policies of every installed plugin that
// declares @require_api docker, disabled plugins included.
func (s *Service) DockerPolicies(ctx context.Context) ([]dockerhost.Policy, error) {
	records, err := s.ListSQLite(ctx)
	if err != nil {
		return nil, err
	}
	policies := []dockerhost.Policy{}
	for _, record := range records {
		policy, declared, err := s.dockerPolicy(record)
		if err != nil {
			return nil, err
		}
		if declared {
			policies = append(policies, policy)
		}
	}
	return policies, nil
}

// PluginForType returns the installed plugin that registers typeKey,
// enabled or disabled, or nil when none does.
func (s *Service) PluginForType(ctx context.Context, typeKey string) (*PluginRecord, error) {
	records, err := s.ListSQLite(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		for _, key := range record.TypeKeys {
			if key == typeKey {
				return record, nil
			}
		}
	}
	return nil, nil
}

// DockerPolicy returns the Docker policy of one installed plugin, or false
// when it does not declare @require_api docker.
func (s *Service) DockerPolicy(ctx context.Context, pluginID string) (dockerhost.Policy, bool, error) {
	record, err := s.GetSQLite(ctx, pluginID)
	if err != nil {
		return dockerhost.Policy{}, false, err
	}
	return s.dockerPolicy(record)
}

// pushDockerFault pushes nil plus the fault table. Spec errors raise.
func pushDockerFault(L *lua.LState, err error) int {
	var specErr *dockerhost.SpecError
	if errors.As(err, &specErr) {
		L.RaiseError("llm_router.docker: %s", specErr.Message)
		return 0
	}
	L.Push(lua.LNil)
	L.Push(dockerFaultTable(L, err))
	return 2
}

// dockerFaultTable renders a runtime failure as an error-contract table
// with docker = true and reason set.
func dockerFaultTable(L *lua.LState, err error) *lua.LTable {
	fault, ok := dockerhost.AsFault(err)
	if !ok {
		fault = &dockerhost.Fault{Reason: dockerhost.ReasonDaemonError, Message: err.Error(), Cause: err}
	}
	table := L.NewTable()
	table.RawSetString("message", lua.LString(fault.Message))
	table.RawSetString("code", lua.LString("server_error"))
	table.RawSetString("status", lua.LNumber(fault.Status()))
	table.RawSetString("docker", lua.LTrue)
	table.RawSetString("reason", lua.LString(fault.Reason))
	return table
}

// pushDockerResult pushes value through JSON plus nil, or the fault.
func pushDockerResult(L *lua.LState, value any, err error) int {
	if err != nil {
		return pushDockerFault(L, err)
	}
	L.Push(goToLuaJSON(L, value))
	L.Push(lua.LNil)
	return 2
}

// dockerOptions reads an optional options table at idx.
func dockerOptions(L *lua.LState, idx int, caller string) *lua.LTable {
	value := L.Get(idx)
	if value == lua.LNil {
		return L.NewTable()
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		L.RaiseError("llm_router.docker.%s: options must be a table", caller)
		return nil
	}
	return table
}

// optionalNumber reads a whole number option within bounds.
func optionalNumber(L *lua.LState, options *lua.LTable, name, caller string, fallback, minimum, maximum int) int {
	value := options.RawGetString(name)
	if value == lua.LNil {
		return fallback
	}
	number, ok := value.(lua.LNumber)
	if !ok || float64(number) != float64(int(number)) || int(number) < minimum || int(number) > maximum {
		L.RaiseError("llm_router.docker.%s: %s must be a whole number from %d to %d", caller, name, minimum, maximum)
		return fallback
	}
	return int(number)
}

// checkOptionNames raises on unknown option keys.
func checkOptionNames(L *lua.LState, options *lua.LTable, caller string, allowed ...string) {
	known := map[string]bool{}
	for _, name := range allowed {
		known[name] = true
	}
	options.ForEach(func(key, _ lua.LValue) {
		name, ok := key.(lua.LString)
		if !ok || !known[string(name)] {
			L.RaiseError("llm_router.docker.%s: unknown option %s", caller, key.String())
		}
	})
}

// specArrayFields are spec fields that hold arrays: an empty Lua table
// encodes as an object and must read as an empty array.
var specArrayFields = []string{"command", "entrypoint", "ports"}

// decodeStrict decodes a generic value into target, rejecting unknown
// fields.
func decodeStrict(value any, target any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = normalizeEmptyObjects(raw, specArrayFields...)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// containerSpecFrom reads one container spec table. A depends_on array
// becomes a map with the started condition.
func containerSpecFrom(L *lua.LState, value lua.LValue, field string) dockerhost.ContainerSpec {
	table, ok := value.(*lua.LTable)
	if !ok {
		L.RaiseError("llm_router.docker: %s must be a table", field)
		return dockerhost.ContainerSpec{}
	}
	generic, err := tableToGo(table)
	if err != nil {
		L.RaiseError("llm_router.docker: %s: %s", field, err.Error())
		return dockerhost.ContainerSpec{}
	}
	if object, ok := generic.(map[string]any); ok {
		if names, isList := object["depends_on"].([]any); isList {
			dependencies := map[string]any{}
			for _, name := range names {
				if text, isText := name.(string); isText {
					dependencies[text] = dockerhost.ConditionStarted
				}
			}
			object["depends_on"] = dependencies
		}
	}
	var spec dockerhost.ContainerSpec
	if err := decodeStrict(generic, &spec); err != nil {
		L.RaiseError("llm_router.docker: %s: %s", field, err.Error())
	}
	return spec
}

// projectFrom reads a compose project: {services = {...}} or
// {yaml = text, variables = {...}}.
func projectFrom(L *lua.LState, bridge *dockerBridge, value lua.LValue) dockerhost.ProjectSpec {
	table, ok := value.(*lua.LTable)
	if !ok {
		L.RaiseError("llm_router.docker.compose: project must be a table")
		return dockerhost.ProjectSpec{}
	}
	if text, isYAML := table.RawGetString("yaml").(lua.LString); isYAML {
		checkOptionNames(L, table, "compose", "yaml", "variables")
		variables := map[string]string{}
		if raw := table.RawGetString("variables"); raw != lua.LNil {
			variablesTable, ok := raw.(*lua.LTable)
			if !ok {
				L.RaiseError("llm_router.docker.compose: variables must be a table")
				return dockerhost.ProjectSpec{}
			}
			variablesTable.ForEach(func(key, value lua.LValue) {
				name, nameOK := key.(lua.LString)
				if !nameOK {
					L.RaiseError("llm_router.docker.compose: variables need string names")
					return
				}
				switch typed := value.(type) {
				case lua.LString:
					variables[string(name)] = string(typed)
				case lua.LNumber, lua.LBool:
					variables[string(name)] = typed.String()
				default:
					L.RaiseError("llm_router.docker.compose: variable %s must be a string, number or boolean", string(name))
				}
			})
		}
		project, err := dockerhost.ParseCompose(string(text), variables, bridge.policy)
		if err != nil {
			L.RaiseError("llm_router.docker.compose: %s", err.Error())
			return dockerhost.ProjectSpec{}
		}
		return project
	}
	checkOptionNames(L, table, "compose", "services")
	servicesTable, ok := table.RawGetString("services").(*lua.LTable)
	if !ok {
		L.RaiseError("llm_router.docker.compose: project needs services or yaml")
		return dockerhost.ProjectSpec{}
	}
	project := dockerhost.ProjectSpec{Services: map[string]dockerhost.ContainerSpec{}}
	servicesTable.ForEach(func(key, value lua.LValue) {
		name, ok := key.(lua.LString)
		if !ok {
			L.RaiseError("llm_router.docker.compose: services need string names")
			return
		}
		project.Services[string(name)] = containerSpecFrom(L, value, "services."+string(name))
	})
	return project
}

// stringList reads a Lua array of strings.
func stringList(L *lua.LState, value lua.LValue, caller, name string) []string {
	table, ok := value.(*lua.LTable)
	if !ok {
		L.RaiseError("llm_router.docker.%s: %s must be an array of strings", caller, name)
		return nil
	}
	values := []string{}
	for index := 1; index <= table.Len(); index++ {
		text, ok := table.RawGetInt(index).(lua.LString)
		if !ok {
			L.RaiseError("llm_router.docker.%s: %s must be an array of strings", caller, name)
			return nil
		}
		values = append(values, string(text))
	}
	return values
}

// newDockerTable builds llm_router.docker for one execution.
func newDockerTable(L *lua.LState, ctx *executionContext) *lua.LTable {
	bridge := ctx.docker
	table := L.NewTable()
	callContext := ctx.callContext
	unavailable := func(L *lua.LState) bool {
		if bridge.service == nil {
			L.Push(lua.LNil)
			L.Push(dockerFaultTable(L, &dockerhost.Fault{Reason: dockerhost.ReasonUnconfigured, Message: "docker is not available in this router"}))
			return true
		}
		return false
	}
	set := func(name string, function lua.LGFunction) {
		table.RawSetString(name, L.NewFunction(function))
	}

	set("ensure", func(L *lua.LState) int {
		key := L.CheckString(1)
		spec := containerSpecFrom(L, L.Get(2), key)
		if unavailable(L) {
			return 2
		}
		status, err := bridge.service.Ensure(callContext(), bridge.policy, key, spec)
		return pushDockerResult(L, status, err)
	})
	set("compose", func(L *lua.LState) int {
		key := L.CheckString(1)
		if bridge.service == nil {
			unavailable(L)
			return 2
		}
		project := projectFrom(L, bridge, L.Get(2))
		status, err := bridge.service.Compose(callContext(), bridge.policy, key, project)
		return pushDockerResult(L, status, err)
	})
	set("start", func(L *lua.LState) int {
		target := L.CheckString(1)
		if unavailable(L) {
			return 2
		}
		status, err := bridge.service.Start(callContext(), bridge.policy, target)
		return pushDockerResult(L, status, err)
	})
	set("stop", func(L *lua.LState) int {
		target := L.CheckString(1)
		options := dockerOptions(L, 2, "stop")
		checkOptionNames(L, options, "stop", "timeout_seconds")
		timeout := optionalNumber(L, options, "timeout_seconds", "stop", defaultStopSeconds, 0, maximumStopSeconds)
		if unavailable(L) {
			return 2
		}
		status, err := bridge.service.Stop(callContext(), bridge.policy, target, timeout)
		return pushDockerResult(L, status, err)
	})
	set("remove", func(L *lua.LState) int {
		target := L.CheckString(1)
		options := dockerOptions(L, 2, "remove")
		checkOptionNames(L, options, "remove", "volumes")
		volumes := lua.LVAsBool(options.RawGetString("volumes"))
		if unavailable(L) {
			return 2
		}
		if err := bridge.service.Remove(callContext(), bridge.policy, target, volumes); err != nil {
			return pushDockerFault(L, err)
		}
		L.Push(lua.LTrue)
		L.Push(lua.LNil)
		return 2
	})
	set("status", func(L *lua.LState) int {
		target := L.CheckString(1)
		if unavailable(L) {
			return 2
		}
		status, err := bridge.service.Status(callContext(), bridge.policy, target)
		return pushDockerResult(L, status, err)
	})
	set("list", func(L *lua.LState) int {
		if unavailable(L) {
			return 2
		}
		statuses, err := bridge.service.List(callContext(), bridge.policy)
		if err != nil {
			return pushDockerFault(L, err)
		}
		list := L.NewTable()
		for _, status := range statuses {
			list.Append(goToLuaJSON(L, status))
		}
		L.Push(list)
		L.Push(lua.LNil)
		return 2
	})
	set("logs", func(L *lua.LState) int {
		target := L.CheckString(1)
		options := dockerOptions(L, 2, "logs")
		checkOptionNames(L, options, "logs", "tail")
		tail := optionalNumber(L, options, "tail", "logs", defaultDockerLogTail, 1, maximumDockerLogTail)
		if unavailable(L) {
			return 2
		}
		text, err := bridge.service.Logs(callContext(), bridge.policy, target, tail)
		if err != nil {
			return pushDockerFault(L, err)
		}
		L.Push(lua.LString(text))
		L.Push(lua.LNil)
		return 2
	})
	set("wait", func(L *lua.LState) int {
		target := L.CheckString(1)
		options := dockerOptions(L, 2, "wait")
		checkOptionNames(L, options, "wait", "port", "timeout_ms")
		port := optionalNumber(L, options, "port", "wait", 0, 1, 65535)
		timeout := optionalNumber(L, options, "timeout_ms", "wait", defaultDockerWaitMs, 100, maximumDockerWaitMs)
		if unavailable(L) {
			return 2
		}
		status, err := bridge.service.Wait(callContext(), bridge.policy, target, port, time.Duration(timeout)*time.Millisecond)
		return pushDockerResult(L, status, err)
	})
	set("exec", func(L *lua.LState) int {
		target := L.CheckString(1)
		command := stringList(L, L.Get(2), "exec", "command")
		options := dockerOptions(L, 3, "exec")
		checkOptionNames(L, options, "exec", "timeout_ms", "environment", "user", "working_directory", "stdin")
		timeout := optionalNumber(L, options, "timeout_ms", "exec", defaultDockerExecMs, 100, maximumDockerWaitMs)
		commandOptions := dockerhost.CommandOptions{
			User:             luaTableString(options, "user", ""),
			WorkingDirectory: luaTableString(options, "working_directory", ""),
			Environment:      luaTableStringMap(options, "environment"),
			Timeout:          time.Duration(timeout) * time.Millisecond,
		}
		if stdin, ok := options.RawGetString("stdin").(lua.LString); ok {
			text := string(stdin)
			commandOptions.Stdin = &text
		}
		if unavailable(L) {
			return 2
		}
		result, err := bridge.service.RunCommand(callContext(), bridge.policy, target, command, commandOptions)
		return pushDockerResult(L, result, err)
	})
	set("open_http", func(L *lua.LState) int {
		target := L.CheckString(1)
		port := L.CheckInt(2)
		options := dockerOptions(L, 3, "open_http")
		checkOptionNames(L, options, "open_http", "timeout_ms")
		timeoutMs := optionalNumber(L, options, "timeout_ms", "open_http", defaultContainerHTTPMs, minHTTPTimeoutMs, maxHTTPTimeoutMs)
		if port < 1 || port > 65535 {
			L.RaiseError("llm_router.docker.open_http: port %d is out of range", port)
			return 0
		}
		container := dockerhost.UnavailableHTTP()
		if bridge.service != nil {
			opened, err := bridge.service.OpenHTTP(bridge.policy, target, port, time.Duration(timeoutMs)*time.Millisecond)
			if err != nil {
				return pushDockerFault(L, err)
			}
			container = opened
		}
		client := newContainerHTTPClient(ctx, container, timeoutMs)
		L.Push(client.toLua(L))
		return 1
	})
	return table
}
