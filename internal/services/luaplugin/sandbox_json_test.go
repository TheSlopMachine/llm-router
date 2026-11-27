package luaplugin

import (
	"strings"
	"testing"
)

func TestSandboxSHA256Hex(t *testing.T) {
	state := newSandboxState(&executionContext{})
	defer state.Close()
	err := state.DoString(`
assert(llm_router.sha256_hex("abc") == "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
assert(llm_router.sha256_hex("") == "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
`)
	if err != nil {
		t.Fatalf("sha256_hex: %v", err)
	}
}

func TestSandboxJSONNullAndArray(t *testing.T) {
	state := newSandboxState(&executionContext{})
	defer state.Close()
	err := state.DoString(`
local text = json.encode({ id = json.null, files = json.array({}), items = json.array({ 1, 2 }), meta = {} })
assert(text == '{"files":[],"id":null,"items":[1,2],"meta":{}}', text)
assert(json.encode(json.null) == "null")
assert(json.encode({ json.null, 1 }) == "[null,1]")
assert(json.decode("null") == nil)
`)
	if err != nil {
		t.Fatalf("json.null and json.array: %v", err)
	}
}

func TestSandboxJSONArrayRejectsObjectKeys(t *testing.T) {
	state := newSandboxState(&executionContext{})
	defer state.Close()
	err := state.DoString(`json.encode(json.array({ name = "x" }))`)
	if err == nil || !strings.Contains(err.Error(), "json.array table has object keys") {
		t.Fatalf("expected an object-key error, got %v", err)
	}
}

func TestSandboxJSONArrayRejectsForeignMetatable(t *testing.T) {
	state := newSandboxState(&executionContext{})
	defer state.Close()
	err := state.DoString(`json.array(setmetatable({}, {}))`)
	if err == nil || !strings.Contains(err.Error(), "already has a metatable") {
		t.Fatalf("expected a metatable error, got %v", err)
	}
}
