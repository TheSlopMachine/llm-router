package luaplugin

import (
	"bytes"
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// buildMultipart renders a multipart/form-data body from a Lua parts array.
// Each part is {name=..., value="plain field"} or
// {name=..., filename=..., content_type=..., data="raw bytes"}.
// Returns the body and the matching Content-Type header value.
func buildMultipart(parts *lua.LTable) ([]byte, string, error) {
	boundary, err := randomHex(16)
	if err != nil {
		return nil, "", fmt.Errorf("boundary: %w", err)
	}
	var buf bytes.Buffer
	n := parts.Len()
	if n == 0 {
		return nil, "", fmt.Errorf("parts array is empty")
	}
	if n > 64 {
		return nil, "", fmt.Errorf("parts array exceeds 64 entries")
	}
	for i := 1; i <= n; i++ {
		part, ok := parts.RawGetInt(i).(*lua.LTable)
		if !ok {
			return nil, "", fmt.Errorf("part %d must be a table", i)
		}
		name, ok := part.RawGetString("name").(lua.LString)
		if !ok || name == "" {
			return nil, "", fmt.Errorf("part %d: name is required", i)
		}
		buf.WriteString("\r\n--" + boundary + "\r\n")
		data := part.RawGetString("data")
		if data == lua.LNil {
			value, ok := part.RawGetString("value").(lua.LString)
			if !ok {
				return nil, "", fmt.Errorf("part %d (%q): value or data is required", i, name)
			}
			fmt.Fprintf(&buf, "Content-Disposition: form-data; name=%q\r\n\r\n", sanitizeHeaderValue(string(name)))
			buf.WriteString(string(value))
			continue
		}
		dataStr, ok := data.(lua.LString)
		if !ok {
			return nil, "", fmt.Errorf("part %d (%q): data must be a string", i, name)
		}
		filename, _ := part.RawGetString("filename").(lua.LString)
		contentType, ok := part.RawGetString("content_type").(lua.LString)
		if !ok || contentType == "" {
			contentType = "application/octet-stream"
		}
		fmt.Fprintf(&buf, "Content-Disposition: form-data; name=%q; filename=%q\r\n",
			sanitizeHeaderValue(string(name)), sanitizeHeaderValue(string(filename)))
		fmt.Fprintf(&buf, "Content-Type: %s\r\n\r\n", sanitizeHeaderValue(string(contentType)))
		buf.WriteString(string(dataStr))
	}
	buf.WriteString("\r\n--" + boundary + "--\r\n")
	return buf.Bytes(), "multipart/form-data; boundary=" + boundary, nil
}

// sanitizeHeaderValue strips characters that could break out of a quoted
// multipart header parameter.
func sanitizeHeaderValue(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '"', '\r', '\n':
			return -1
		}
		return r
	}, s)
}
