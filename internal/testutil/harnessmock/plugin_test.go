package harnessmock

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// pluginSource is the harness mock plugin under test, relative to this package.
var pluginSource = filepath.Join("..", "..", "..", "test", "harness", "plugins", "harness-mock.lua")

type logEntry struct {
	severity string
	message  string
	keys     map[string]string
}

// pluginRuntime loads the plugin into a gopher-lua state, the same VM the router
// embeds, with the llm_router surface stubbed: registration, logging, storage, json.
type pluginRuntime struct {
	state    *lua.LState
	handlers map[string]*lua.LFunction
	logs     []logEntry
	storage  map[string]any
}

func newPluginRuntime(t *testing.T) *pluginRuntime {
	t.Helper()
	state := lua.NewState()
	t.Cleanup(state.Close)
	runtime := &pluginRuntime{
		state:    state,
		handlers: map[string]*lua.LFunction{},
		storage:  map[string]any{},
	}

	module := state.NewTable()
	state.SetField(module, "register", state.NewFunction(func(L *lua.LState) int {
		L.CheckString(1)
		plugin := L.CheckTable(2)
		for _, name := range []string{"complete", "complete_stream", "get_model_infos"} {
			if handler, ok := plugin.RawGetString(name).(*lua.LFunction); ok {
				runtime.handlers[name] = handler
			}
		}
		return 0
	}))
	state.SetField(module, "log", state.NewFunction(func(L *lua.LState) int {
		entry := logEntry{severity: L.CheckString(1), message: L.CheckString(2), keys: map[string]string{}}
		if keys, ok := L.Get(3).(*lua.LTable); ok {
			keys.ForEach(func(key, value lua.LValue) {
				entry.keys[key.String()] = value.String()
			})
		}
		runtime.logs = append(runtime.logs, entry)
		return 0
	}))

	storage := state.NewTable()
	state.SetField(storage, "get", state.NewFunction(func(L *lua.LState) int {
		key := L.CheckString(1) + "/" + L.CheckString(2)
		stored, found := runtime.storage[key]
		if !found {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(goToLua(L, stored))
		return 1
	}))
	state.SetField(storage, "set", state.NewFunction(func(L *lua.LState) int {
		key := L.CheckString(1) + "/" + L.CheckString(2)
		runtime.storage[key] = luaToGo(L.Get(3))
		L.Push(lua.LTrue)
		return 1
	}))
	state.SetField(module, "storage", storage)
	state.SetGlobal("llm_router", module)

	jsonModule := state.NewTable()
	state.SetField(jsonModule, "encode", state.NewFunction(func(L *lua.LState) int {
		encoded, err := json.Marshal(luaToGo(L.Get(1)))
		if err != nil {
			L.RaiseError("json.encode: %v", err)
			return 0
		}
		L.Push(lua.LString(encoded))
		return 1
	}))
	state.SetGlobal("json", jsonModule)

	if err := state.DoFile(pluginSource); err != nil {
		t.Fatalf("load plugin: %v", err)
	}
	return runtime
}

// complete calls the unary handler and returns the completion or the terminal error.
func (runtime *pluginRuntime) complete(t *testing.T, request map[string]any) (map[string]any, map[string]any) {
	t.Helper()
	state := runtime.state
	err := state.CallByParam(lua.P{Fn: runtime.handlers["complete"], NRet: 2, Protect: true},
		state.NewTable(), goToLua(state, request))
	if err != nil {
		t.Fatalf("complete raised: %v", err)
	}
	completion := asObject(state.Get(-2))
	failure := asObject(state.Get(-1))
	state.Pop(2)
	return completion, failure
}

// stream calls the streaming handler and returns every emitted chunk in order.
func (runtime *pluginRuntime) stream(t *testing.T, request map[string]any) []map[string]any {
	t.Helper()
	state := runtime.state
	var chunks []map[string]any
	emit := state.NewFunction(func(L *lua.LState) int {
		chunk, ok := tableToGo(L.CheckTable(1)).(map[string]any)
		if !ok {
			L.RaiseError("chunk must be an object")
			return 0
		}
		chunks = append(chunks, chunk)
		return 0
	})
	err := state.CallByParam(lua.P{Fn: runtime.handlers["complete_stream"], NRet: 0, Protect: true},
		state.NewTable(), goToLua(state, request), emit)
	if err != nil {
		t.Fatalf("complete_stream raised: %v", err)
	}
	return chunks
}

// modelNames returns the model names the plugin declares, sorted.
func (runtime *pluginRuntime) modelNames(t *testing.T) []string {
	t.Helper()
	state := runtime.state
	if err := state.CallByParam(lua.P{Fn: runtime.handlers["get_model_infos"], NRet: 1, Protect: true}, state.NewTable()); err != nil {
		t.Fatalf("get_model_infos raised: %v", err)
	}
	list, ok := tableToGo(state.Get(-1).(*lua.LTable)).([]any)
	state.Pop(1)
	if !ok {
		t.Fatalf("get_model_infos must return a list")
	}
	var names []string
	for _, entry := range list {
		names = append(names, entry.(map[string]any)["name"].(string))
	}
	sort.Strings(names)
	return names
}

func asObject(value lua.LValue) map[string]any {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil
	}
	object, _ := tableToGo(table).(map[string]any)
	return object
}

func goToLua(state *lua.LState, value any) lua.LValue {
	switch typed := value.(type) {
	case nil:
		return lua.LNil
	case string:
		return lua.LString(typed)
	case int:
		return lua.LNumber(typed)
	case float64:
		return lua.LNumber(typed)
	case bool:
		return lua.LBool(typed)
	case []any:
		table := state.NewTable()
		for index, item := range typed {
			table.RawSetInt(index+1, goToLua(state, item))
		}
		return table
	case map[string]any:
		table := state.NewTable()
		for key, item := range typed {
			table.RawSetString(key, goToLua(state, item))
		}
		return table
	}
	panic(fmt.Sprintf("goToLua: unsupported %T", value))
}

func luaToGo(value lua.LValue) any {
	switch typed := value.(type) {
	case lua.LString:
		return string(typed)
	case lua.LNumber:
		return float64(typed)
	case lua.LBool:
		return bool(typed)
	case *lua.LTable:
		return tableToGo(typed)
	}
	return nil
}

// tableToGo maps a Lua table to a list when its keys are 1..n, else to an object.
func tableToGo(table *lua.LTable) any {
	var numericKeys []int
	allNumeric := true
	table.ForEach(func(key, _ lua.LValue) {
		number, ok := key.(lua.LNumber)
		if !ok {
			allNumeric = false
			return
		}
		numericKeys = append(numericKeys, int(number))
	})
	if allNumeric && len(numericKeys) > 0 {
		sort.Ints(numericKeys)
		contiguous := true
		for index, key := range numericKeys {
			if key != index+1 {
				contiguous = false
				break
			}
		}
		if contiguous {
			values := make([]any, len(numericKeys))
			for index := range numericKeys {
				values[index] = luaToGo(table.RawGetInt(index + 1))
			}
			return values
		}
	}
	object := map[string]any{}
	table.ForEach(func(key, value lua.LValue) {
		object[key.String()] = luaToGo(value)
	})
	return object
}

func chatRequest(modelName string, toolNames []string, messages []any) map[string]any {
	tools := make([]any, 0, len(toolNames))
	for _, name := range toolNames {
		tools = append(tools, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": name, "parameters": map[string]any{"type": "object"}},
		})
	}
	return map[string]any{
		"model":      "harness/" + modelName,
		"model_name": modelName,
		"cache_key":  "pck-test",
		"messages":   messages,
		"tools":      tools,
	}
}

func userTurn(text string) map[string]any {
	return map[string]any{"role": "user", "content": text}
}

func assistantText(text string) map[string]any {
	return map[string]any{"role": "assistant", "content": text}
}

func assistantCall(callID, toolName, arguments string) map[string]any {
	return map[string]any{
		"role": "assistant",
		"tool_calls": []any{map[string]any{
			"id":       callID,
			"type":     "function",
			"function": map[string]any{"name": toolName, "arguments": arguments},
		}},
	}
}

func toolTurn(callID, output string) map[string]any {
	return map[string]any{"role": "tool", "tool_call_id": callID, "content": output}
}

func assistantMessageOf(t *testing.T, completion map[string]any) map[string]any {
	t.Helper()
	choices, ok := completion["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("choices: %+v", completion)
	}
	message, ok := choices[0].(map[string]any)["message"].(map[string]any)
	if !ok {
		t.Fatalf("message: %+v", choices[0])
	}
	return message
}

// toolCallOf returns the single tool call of a completion.
func toolCallOf(t *testing.T, completion map[string]any) (string, string, string) {
	t.Helper()
	calls, ok := assistantMessageOf(t, completion)["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("expected one tool call: %+v", completion)
	}
	call := calls[0].(map[string]any)
	function := call["function"].(map[string]any)
	return call["id"].(string), function["name"].(string), function["arguments"].(string)
}

func replyTextOf(t *testing.T, completion map[string]any) string {
	t.Helper()
	text, ok := assistantMessageOf(t, completion)["content"].(string)
	if !ok {
		t.Fatalf("reply has no content: %+v", completion)
	}
	return text
}

func argumentsOf(t *testing.T, arguments string) map[string]any {
	t.Helper()
	var decoded map[string]any
	if err := json.Unmarshal([]byte(arguments), &decoded); err != nil {
		t.Fatalf("arguments are not JSON: %v (%s)", err, arguments)
	}
	return decoded
}

func failureReasonOf(t *testing.T, failure map[string]any) string {
	t.Helper()
	if failure == nil {
		t.Fatalf("expected a terminal error, got success")
	}
	message, _ := failure["message"].(string)
	return message
}

func TestAuxiliaryRequestWithoutToolsAnswersText(t *testing.T) {
	runtime := newPluginRuntime(t)
	completion, failure := runtime.complete(t, chatRequest("claude-sonnet-4-5", nil,
		[]any{userTurn("Generate a title for this conversation: PHASE 1 run=ci-1")}))
	if failure != nil {
		t.Fatalf("auxiliary request failed: %+v", failure)
	}
	if got := replyTextOf(t, completion); got != "harness-mock auxiliary" {
		t.Fatalf("auxiliary reply: %q", got)
	}
}

func TestPhaseOneIssuesShellCallThenCompletes(t *testing.T) {
	runtime := newPluginRuntime(t)
	start := []any{userTurn("PHASE 1 run=ci-1: start")}

	completion, failure := runtime.complete(t, chatRequest("claude-sonnet-4-5", []string{"Read", "Write", "Bash"}, start))
	if failure != nil {
		t.Fatalf("phase 1 call failed: %+v", failure)
	}
	callID, name, arguments := toolCallOf(t, completion)
	if name != "Bash" || callID != "harness_p1_m1" {
		t.Fatalf("call: id=%s name=%s", callID, name)
	}
	if command, _ := argumentsOf(t, arguments)["command"].(string); !strings.Contains(command, "phase1.txt") {
		t.Fatalf("command: %q", command)
	}

	history := append(start, assistantCall(callID, name, arguments), toolTurn(callID, "phase-1 ok\n"))
	completion, failure = runtime.complete(t, chatRequest("claude-sonnet-4-5", []string{"Read", "Write", "Bash"}, history))
	if failure != nil {
		t.Fatalf("phase 1 completion failed: %+v", failure)
	}
	if got := replyTextOf(t, completion); got != "PHASE 1 COMPLETE" {
		t.Fatalf("reply: %q", got)
	}
}

func TestToolResultFromAnotherPhaseFails(t *testing.T) {
	runtime := newPluginRuntime(t)
	history := []any{
		userTurn("PHASE 1 run=ci-1: start"),
		assistantCall("harness_p2_m1", "Bash", `{"command":"true"}`),
		toolTurn("harness_p2_m1", "phase-1 ok\n"),
	}
	_, failure := runtime.complete(t, chatRequest("claude-sonnet-4-5", []string{"Bash"}, history))
	if reason := failureReasonOf(t, failure); !strings.Contains(reason, "tool_result_phase_mismatch") {
		t.Fatalf("expected phase mismatch, got %q", reason)
	}
}

func TestMissingExpectedOutputFails(t *testing.T) {
	runtime := newPluginRuntime(t)
	history := []any{
		userTurn("PHASE 1 run=ci-1: start"),
		assistantCall("harness_p1_m1", "Bash", `{"command":"true"}`),
		toolTurn("harness_p1_m1", "nothing useful"),
	}
	_, failure := runtime.complete(t, chatRequest("claude-sonnet-4-5", []string{"Bash"}, history))
	if reason := failureReasonOf(t, failure); !strings.Contains(reason, "expected_output_missing") {
		t.Fatalf("expected missing output, got %q", reason)
	}
}

func TestTrailingUserTextAfterValidResultCompletes(t *testing.T) {
	runtime := newPluginRuntime(t)
	history := []any{
		userTurn("PHASE 3 run=ci-9: start"),
		assistantCall("harness_p3_m1", "Bash", `{"command":"true"}`),
		toolTurn("harness_p3_m1", "phase-1 ok\nphase-2 ok\n"),
		userTurn("reminder: answer with the phase token"),
	}
	completion, failure := runtime.complete(t, chatRequest("claude-sonnet-4-5", []string{"Bash"}, history))
	if failure != nil {
		t.Fatalf("idempotent completion failed: %+v", failure)
	}
	if got := replyTextOf(t, completion); got != "HARNESS-MOCK-OK" {
		t.Fatalf("reply: %q", got)
	}
}

func TestTrailingUserTextWithoutValidResultIssuesCall(t *testing.T) {
	runtime := newPluginRuntime(t)
	history := []any{
		userTurn("PHASE 3 run=ci-9: start"),
		assistantCall("harness_p3_m1", "Bash", `{"command":"true"}`),
		toolTurn("harness_p3_m1", "nothing useful"),
		userTurn("reminder: answer with the phase token"),
	}
	completion, failure := runtime.complete(t, chatRequest("claude-sonnet-4-5", []string{"Bash"}, history))
	if failure != nil {
		t.Fatalf("call after invalid result failed: %+v", failure)
	}
	if _, name, _ := toolCallOf(t, completion); name != "Bash" {
		t.Fatalf("expected a new Bash call, got tool %s", name)
	}
}

func TestRepeatedCallsTripLoopGuard(t *testing.T) {
	runtime := newPluginRuntime(t)
	history := []any{userTurn("PHASE 1 run=ci-1: start")}
	request := chatRequest("claude-sonnet-4-5", []string{"Bash"}, history)
	for attempt := 1; attempt <= 2; attempt++ {
		if _, failure := runtime.complete(t, request); failure != nil {
			t.Fatalf("attempt %d should still issue a call: %+v", attempt, failure)
		}
	}
	_, failure := runtime.complete(t, request)
	if reason := failureReasonOf(t, failure); !strings.Contains(reason, "tool_call_repeated") {
		t.Fatalf("third attempt should trip the loop guard, got %q", reason)
	}
}

func TestCodexProfileArgumentsFollowTheAdvertisedTool(t *testing.T) {
	runtime := newPluginRuntime(t)
	history := []any{userTurn("PHASE 1 run=ci-1: start")}

	completion, failure := runtime.complete(t, chatRequest("codex-harness", []string{"write_stdin", "exec_command"}, history))
	if failure != nil {
		t.Fatalf("exec_command call failed: %+v", failure)
	}
	_, name, arguments := toolCallOf(t, completion)
	if name != "exec_command" {
		t.Fatalf("tool: %s", name)
	}
	if _, ok := argumentsOf(t, arguments)["cmd"].(string); !ok {
		t.Fatalf("exec_command takes cmd: %s", arguments)
	}

	runtime = newPluginRuntime(t)
	completion, failure = runtime.complete(t, chatRequest("codex-harness", []string{"shell"}, history))
	if failure != nil {
		t.Fatalf("shell call failed: %+v", failure)
	}
	_, name, arguments = toolCallOf(t, completion)
	command, ok := argumentsOf(t, arguments)["command"].([]any)
	if name != "shell" || !ok || len(command) != 3 || command[0] != "bash" || command[1] != "-lc" {
		t.Fatalf("shell takes a command array: name=%s args=%s", name, arguments)
	}
}

func TestOpenCodeProfileArgumentsUseCommandString(t *testing.T) {
	runtime := newPluginRuntime(t)
	completion, failure := runtime.complete(t, chatRequest("opencode-harness", []string{"read", "bash"},
		[]any{userTurn("PHASE 1 run=ci-1: start")}))
	if failure != nil {
		t.Fatalf("bash call failed: %+v", failure)
	}
	_, name, arguments := toolCallOf(t, completion)
	if command, _ := argumentsOf(t, arguments)["command"].(string); name != "bash" || command == "" {
		t.Fatalf("bash call: name=%s args=%s", name, arguments)
	}
}

func TestUnknownModelAndMissingShellToolFail(t *testing.T) {
	runtime := newPluginRuntime(t)
	history := []any{userTurn("PHASE 1 run=ci-1: start")}

	_, failure := runtime.complete(t, chatRequest("gpt-unknown", []string{"Bash"}, history))
	if reason := failureReasonOf(t, failure); !strings.Contains(reason, "unknown_model") {
		t.Fatalf("unknown model: %q", reason)
	}
	_, failure = runtime.complete(t, chatRequest("claude-sonnet-4-5", []string{"Read"}, history))
	if reason := failureReasonOf(t, failure); !strings.Contains(reason, "shell_tool_missing") {
		t.Fatalf("missing shell tool: %q", reason)
	}
}

func TestEveryDeclaredModelHasAShellProfile(t *testing.T) {
	declared := newPluginRuntime(t).modelNames(t)
	want := []string{"claude-sonnet-4-5", "codex-harness", "opencode-harness"}
	if !reflect.DeepEqual(declared, want) {
		t.Fatalf("declared models: got %v want %v", declared, want)
	}
	tools := map[string]string{
		"claude-sonnet-4-5": "Bash",
		"codex-harness":     "exec_command",
		"opencode-harness":  "bash",
	}
	for _, modelName := range declared {
		runtime := newPluginRuntime(t)
		completion, failure := runtime.complete(t, chatRequest(modelName, []string{tools[modelName]},
			[]any{userTurn("PHASE 1 run=ci-1: start")}))
		if failure != nil {
			t.Fatalf("%s has no working profile: %+v", modelName, failure)
		}
		if _, name, _ := toolCallOf(t, completion); name != tools[modelName] {
			t.Fatalf("%s: tool %s", modelName, name)
		}
	}
}

func TestStreamEmitsToolCallChunksInOrder(t *testing.T) {
	runtime := newPluginRuntime(t)
	chunks := runtime.stream(t, chatRequest("codex-harness", []string{"exec_command"},
		[]any{userTurn("PHASE 1 run=ci-1: start")}))
	if len(chunks) != 3 {
		t.Fatalf("chunk count: %d", len(chunks))
	}
	opening := chunks[0]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if opening["id"] != "harness_p1_m1" || opening["function"].(map[string]any)["name"] != "exec_command" {
		t.Fatalf("opening chunk: %+v", opening)
	}
	arguments := chunks[1]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if arguments["id"] != "harness_p1_m1" {
		t.Fatalf("continuation chunk id: %+v", arguments)
	}
	if _, ok := argumentsOf(t, arguments["function"].(map[string]any)["arguments"].(string))["cmd"].(string); !ok {
		t.Fatalf("argument chunk: %s", arguments["function"])
	}
	finish := chunks[2]["choices"].([]any)[0].(map[string]any)
	if finish["finish_reason"] != "tool_calls" || chunks[2]["usage"] == nil {
		t.Fatalf("finish chunk: %+v", chunks[2])
	}
}

func TestStreamEmitsTextChunks(t *testing.T) {
	runtime := newPluginRuntime(t)
	history := []any{
		userTurn("PHASE 1 run=ci-1: start"),
		assistantCall("harness_p1_m1", "Bash", `{"command":"true"}`),
		toolTurn("harness_p1_m1", "phase-1 ok\n"),
	}
	chunks := runtime.stream(t, chatRequest("claude-sonnet-4-5", []string{"Bash"}, history))
	if len(chunks) != 2 {
		t.Fatalf("chunk count: %d", len(chunks))
	}
	delta := chunks[0]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	if delta["content"] != "PHASE 1 COMPLETE" {
		t.Fatalf("text chunk: %+v", delta)
	}
	if chunks[1]["choices"].([]any)[0].(map[string]any)["finish_reason"] != "stop" {
		t.Fatalf("finish chunk: %+v", chunks[1])
	}
}

// TestThreePhaseScenarioPerHarness drives the full scenario the way a harness does:
// each phase adds a user turn, the model issues one shell call, the harness returns
// the shell output, and the model answers with the phase token.
func TestThreePhaseScenarioPerHarness(t *testing.T) {
	scenarios := []struct {
		model string
		tools []string
		shell string
	}{
		{model: "claude-sonnet-4-5", tools: []string{"Read", "Write", "Bash"}, shell: "Bash"},
		{model: "codex-harness", tools: []string{"exec_command", "write_stdin", "view_image"}, shell: "exec_command"},
		{model: "opencode-harness", tools: []string{"bash", "edit", "glob", "grep", "read", "write"}, shell: "bash"},
	}
	outputs := []string{"phase-1 ok\n", "phase-1 ok\nphase-2 ok\n", "phase-1 ok\nphase-2 ok\n"}
	replies := []string{"PHASE 1 COMPLETE", "PHASE 2 COMPLETE", "HARNESS-MOCK-OK"}

	for _, scenario := range scenarios {
		t.Run(scenario.model, func(t *testing.T) {
			runtime := newPluginRuntime(t)
			history := []any{userTurn("PHASE 1 run=ci-7: start")}
			seenCallIDs := map[string]bool{}

			for phase := 1; phase <= 3; phase++ {
				if phase > 1 {
					history = append(history, userTurn(fmt.Sprintf("PHASE %d run=ci-7: continue", phase)))
				}
				request := chatRequest(scenario.model, scenario.tools, history)
				completion, failure := runtime.complete(t, request)
				if failure != nil {
					t.Fatalf("phase %d call failed: %+v", phase, failure)
				}
				callID, name, arguments := toolCallOf(t, completion)
				if name != scenario.shell || seenCallIDs[callID] {
					t.Fatalf("phase %d: tool %s id %s (seen before: %v)", phase, name, callID, seenCallIDs[callID])
				}
				seenCallIDs[callID] = true
				history = append(history, assistantCall(callID, name, arguments), toolTurn(callID, outputs[phase-1]))

				completion, failure = runtime.complete(t, chatRequest(scenario.model, scenario.tools, history))
				if failure != nil {
					t.Fatalf("phase %d completion failed: %+v", phase, failure)
				}
				if got := replyTextOf(t, completion); got != replies[phase-1] {
					t.Fatalf("phase %d reply: %q", phase, got)
				}
				history = append(history, assistantText(replies[phase-1]))
			}

			var sequence []string
			var callMessages []int
			for _, entry := range runtime.logs {
				if entry.severity == "error" {
					t.Fatalf("unexpected error log: %+v", entry)
				}
				if entry.keys["run"] != "ci-7" {
					t.Fatalf("log without run key: %+v", entry)
				}
				sequence = append(sequence, entry.keys["phase"]+":"+entry.keys["step"])
				if entry.keys["step"] == "call" {
					var count int
					fmt.Sscanf(entry.keys["messages"], "%d", &count)
					callMessages = append(callMessages, count)
				}
			}
			wantSequence := []string{"1:call", "1:complete", "2:call", "2:complete", "3:call", "3:complete"}
			if !reflect.DeepEqual(sequence, wantSequence) {
				t.Fatalf("phase log sequence: got %v want %v", sequence, wantSequence)
			}
			for index := 1; index < len(callMessages); index++ {
				if callMessages[index] <= callMessages[index-1] {
					t.Fatalf("history did not grow across phases: %v", callMessages)
				}
			}
		})
	}
}
