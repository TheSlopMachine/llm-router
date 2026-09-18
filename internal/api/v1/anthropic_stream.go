package v1

// OpenAI-SSE → Anthropic-events stream translator for POST /v1/messages.
//
// router.CompleteStream writes OpenAI `data: {chunk}` lines into this
// writer; the translator re-emits Anthropic message_start / content_block_*
// / message_delta / message_stop events to the client. State is per stream:
// text, thinking and tool_use blocks open and close in arrival order.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

type anthropicStreamWriter struct {
	w       io.Writer
	flusher interface{ Flush() }
	model   string

	started      bool
	messageID    string
	blockType    string // "", "text", "thinking", "tool_use"
	blockIndex   int    // Anthropic block index of the open block
	nextBlock    int    // next Anthropic block index to assign
	toolBlocks   map[int]int
	openToolIdx  int // OpenAI tool_calls index of the open tool block
	finishReason string
	inputTokens  int
	outputTokens int

	buf []byte
}

func newAnthropicStreamWriter(w io.Writer, flusher interface{ Flush() }, model string) *anthropicStreamWriter {
	return &anthropicStreamWriter{
		w:           w,
		flusher:     flusher,
		model:       model,
		toolBlocks:  map[int]int{},
		openToolIdx: -1,
	}
}

func (s *anthropicStreamWriter) emit(event string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

// Write consumes OpenAI SSE bytes. Complete data frames are translated
// incrementally; a trailing partial frame stays buffered.
func (s *anthropicStreamWriter) Write(p []byte) (int, error) {
	s.buf = append(s.buf, p...)
	for {
		idx := bytes.Index(s.buf, []byte("\n\n"))
		if idx < 0 {
			break
		}
		frame := s.buf[:idx]
		s.buf = s.buf[idx+2:]
		if err := s.handleFrame(frame); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (s *anthropicStreamWriter) handleFrame(frame []byte) error {
	for _, line := range bytes.Split(frame, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		data := bytes.TrimPrefix(line, []byte("data: "))
		if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			return s.finish()
		}
		var chunk models.StreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			continue
		}
		if err := s.handleChunk(&chunk); err != nil {
			return err
		}
	}
	return nil
}

func (s *anthropicStreamWriter) handleChunk(chunk *models.StreamChunk) error {
	if !s.started {
		s.messageID = chunk.ID
		if s.messageID == "" {
			s.messageID = "msg_router"
		}
		if chunk.Usage != nil {
			s.inputTokens = chunk.Usage.PromptTokens
		}
		if err := s.emit("message_start", map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            s.messageID,
				"type":          "message",
				"role":          "assistant",
				"model":         s.model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage":         map[string]any{"input_tokens": s.inputTokens, "output_tokens": 0},
			},
		}); err != nil {
			return err
		}
		s.started = true
	}
	if chunk.Usage != nil {
		if chunk.Usage.PromptTokens > 0 {
			s.inputTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			s.outputTokens = chunk.Usage.CompletionTokens
		}
	}
	for _, choice := range chunk.Choices {
		if err := s.handleDelta(choice); err != nil {
			return err
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			s.finishReason = *choice.FinishReason
		}
	}
	return nil
}

func (s *anthropicStreamWriter) handleDelta(choice models.StreamChunkChoice) error {
	delta := choice.Delta
	if strings.TrimSpace(delta.ReasoningContent) != "" {
		if err := s.openBlock("thinking", 0, "", ""); err != nil {
			return err
		}
		if err := s.emit("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": s.blockIndex,
			"delta": map[string]any{"type": "thinking_delta", "thinking": delta.ReasoningContent},
		}); err != nil {
			return err
		}
	}
	if strings.TrimSpace(delta.Content) != "" {
		if err := s.openBlock("text", 0, "", ""); err != nil {
			return err
		}
		if err := s.emit("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": s.blockIndex,
			"delta": map[string]any{"type": "text_delta", "text": delta.Content},
		}); err != nil {
			return err
		}
	}
	for i, tc := range delta.ToolCalls {
		toolIdx := i
		if tc.ID != "" {
			if mapped, ok := s.toolBlocks[toolIdx]; !ok || s.blockType != "tool_use" || s.openToolIdx != toolIdx || s.blockIndex != mapped {
				if err := s.openBlock("tool_use", toolIdx, tc.ID, tc.Function.Name); err != nil {
					return err
				}
			}
		}
		if tc.Function.Arguments != "" && s.blockType == "tool_use" {
			if err := s.emit("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": s.blockIndex,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": tc.Function.Arguments},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// openBlock closes the current block (when the type changes) and opens a new
// one. A block of the same type stays open for consecutive deltas.
func (s *anthropicStreamWriter) openBlock(kind string, toolIdx int, toolID, toolName string) error {
	if s.blockType == kind && kind != "tool_use" {
		return nil
	}
	if s.blockType != "" {
		if err := s.emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": s.blockIndex,
		}); err != nil {
			return err
		}
		s.blockType = ""
	}
	s.blockIndex = s.nextBlock
	s.nextBlock++
	s.blockType = kind
	var contentBlock map[string]any
	switch kind {
	case "thinking":
		contentBlock = map[string]any{"type": "thinking", "thinking": ""}
	case "tool_use":
		contentBlock = map[string]any{"type": "tool_use", "id": toolID, "name": toolName, "input": map[string]any{}}
		s.toolBlocks[toolIdx] = s.blockIndex
		s.openToolIdx = toolIdx
	default:
		contentBlock = map[string]any{"type": "text", "text": ""}
	}
	return s.emit("content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         s.blockIndex,
		"content_block": contentBlock,
	})
}

// finish closes the open block and terminates the message. Called on [DONE].
func (s *anthropicStreamWriter) finish() error {
	if !s.started {
		return nil
	}
	if s.blockType != "" {
		if err := s.emit("content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": s.blockIndex,
		}); err != nil {
			return err
		}
		s.blockType = ""
	}
	stopReason := anthropicFinish(s.finishReason)
	if err := s.emit("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": s.outputTokens},
	}); err != nil {
		return err
	}
	return s.emit("message_stop", map[string]any{"type": "message_stop"})
}
