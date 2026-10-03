package luaplugin

// Handler names a Lua-callable function slot of llm_router.register. Static
// schema tables (credential_schema, config_schema, settings_schema,
// proxy_schema) and colocated job specs are not handlers: they are
// validated at install, never invoked.
type Handler string

// Required handler: every registered type serves it.
const HandlerComplete Handler = "complete"

// Optional handlers: declared per type, validated as functions when present.
const (
	HandlerCompleteStream      Handler = "complete_stream"
	HandlerTranscribe          Handler = "transcribe"
	HandlerSpeech              Handler = "speech"
	HandlerGenerateImage       Handler = "generate_image"
	HandlerEmbed               Handler = "embed"
	HandlerModerate            Handler = "moderate"
	HandlerGenerateVideo       Handler = "generate_video"
	HandlerPollVideo           Handler = "poll_video"
	HandlerVideoContent        Handler = "video_content"
	HandlerValidateCredentials Handler = "validate_credentials"
	HandlerGetModelInfos       Handler = "get_model_infos"
	HandlerCheckHealth         Handler = "check_health"
	HandlerAuthInitiate        Handler = "auth_initiate"
	HandlerAuthStep            Handler = "auth_step"
)

// OptionalHandlerNames lists every non-required handler slot.
func OptionalHandlerNames() []string {
	return []string{
		string(HandlerCompleteStream),
		string(HandlerTranscribe),
		string(HandlerSpeech),
		string(HandlerGenerateImage),
		string(HandlerEmbed),
		string(HandlerModerate),
		string(HandlerGenerateVideo),
		string(HandlerPollVideo),
		string(HandlerVideoContent),
		string(HandlerValidateCredentials),
		string(HandlerGetModelInfos),
		string(HandlerCheckHealth),
		string(HandlerAuthInitiate),
		string(HandlerAuthStep),
	}
}

// AllHandlerNames lists every handler slot, required first.
func AllHandlerNames() []string {
	return append([]string{string(HandlerComplete)}, OptionalHandlerNames()...)
}
