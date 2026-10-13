package luaplugin

// Handler names a Lua-callable function slot of llm_router.register. Static
// schema tables (credential_schema, config_schema, settings_schema,
// proxy_schema) and colocated job specs are not handlers: they are
// validated at install, never invoked.
type Handler string

// Required handler: every registered type serves it.
const HandlerComplete Handler = "complete"

// Lifecycle hooks: optional one-time handlers. initialize runs once per
// plugin event (install, update, router startup); deinitialize runs before
// plugin removal, replacement, provider disable and router shutdown. Both
// are optional and run only when the plugin declares them.
const (
	HandlerInitialize   Handler = "initialize"
	HandlerDeinitialize Handler = "deinitialize"
)

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
	HandlerCheckModel          Handler = "check_model"
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
		string(HandlerCheckModel),
		string(HandlerAuthInitiate),
		string(HandlerAuthStep),
		string(HandlerInitialize),
		string(HandlerDeinitialize),
	}
}

// AllHandlerNames lists every handler slot, required first.
func AllHandlerNames() []string {
	return append([]string{string(HandlerComplete)}, OptionalHandlerNames()...)
}
