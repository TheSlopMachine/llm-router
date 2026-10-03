package luaplugin

// Handler names a Lua-callable slot of llm_router.register. The two handler
// inventories (registration-time validation in sandbox.go and install-time
// discovery in service.go) both derive from these lists.
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
	HandlerClassifyError       Handler = "classify_error"
	HandlerNeedsRefresh        Handler = "needs_refresh"
	HandlerRefreshCredential   Handler = "refresh_credential"
	HandlerCheckHealth         Handler = "check_health"
	HandlerConfigSchema        Handler = "config_schema"
	HandlerCredentialSchema    Handler = "credential_schema"
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
		string(HandlerClassifyError),
		string(HandlerNeedsRefresh),
		string(HandlerRefreshCredential),
		string(HandlerCheckHealth),
		string(HandlerConfigSchema),
		string(HandlerCredentialSchema),
		string(HandlerAuthInitiate),
		string(HandlerAuthStep),
	}
}

// AllHandlerNames lists every handler slot, required first.
func AllHandlerNames() []string {
	return append([]string{string(HandlerComplete)}, OptionalHandlerNames()...)
}
