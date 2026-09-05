package generic

import (
	"strings"

	sdk "github.com/TheSlopMachine/llm-router-sdk"
)

// AuthFlow implements a simple API key authentication flow for custom providers.
type AuthFlow struct{}

func (f *AuthFlow) InitiateFlow(ctx sdk.AuthFlowContext) (sdk.AuthFlowState, error) {
	return sdk.AuthFlowState{
		RenderHTML: `
<div class="auth-flow-content">
	<p><strong>Custom OpenAI-Compatible Provider</strong></p>
	<p>Enter your API key for this provider. The provider must support the OpenAI chat completions API.</p>
	<div class="form-group">
		<label for="api_key">API Key *</label>
		<input type="text" id="api_key" name="api_key" class="form-control" placeholder="sk-..." required />
	</div>
	<button type="submit" class="btn btn-primary">Add Credential</button>
</div>`,
	}, nil
}

func (f *AuthFlow) HandleStep(ctx sdk.AuthFlowContext, input map[string][]string) (sdk.AuthFlowState, error) {
	vals, ok := input["api_key"]
	if !ok || len(vals) == 0 {
		return sdk.AuthFlowState{
			RenderHTML: `
<div class="auth-flow-content">
	<div class="alert alert-danger">API key is required</div>
	<p><strong>Custom OpenAI-Compatible Provider</strong></p>
	<div class="form-group">
		<label for="api_key">API Key *</label>
		<input type="text" id="api_key" name="api_key" class="form-control" placeholder="sk-..." required />
	</div>
	<button type="submit" class="btn btn-primary">Add Credential</button>
</div>`,
		}, nil
	}

	apiKey := strings.TrimSpace(vals[0])
	if apiKey == "" {
		return sdk.AuthFlowState{
			RenderHTML: `
<div class="auth-flow-content">
	<div class="alert alert-danger">API key cannot be empty</div>
	<p><strong>Custom OpenAI-Compatible Provider</strong></p>
	<div class="form-group">
		<label for="api_key">API Key *</label>
		<input type="text" id="api_key" name="api_key" class="form-control" placeholder="sk-..." required />
	</div>
	<button type="submit" class="btn btn-primary">Add Credential</button>
</div>`,
		}, nil
	}

	if len(apiKey) < 8 {
		return sdk.AuthFlowState{
			RenderHTML: `
<div class="auth-flow-content">
	<div class="alert alert-danger">API key appears invalid (too short, minimum 8 characters)</div>
	<p><strong>Custom OpenAI-Compatible Provider</strong></p>
	<div class="form-group">
		<label for="api_key">API Key *</label>
		<input type="text" id="api_key" name="api_key" class="form-control" placeholder="sk-..." required />
	</div>
	<button type="submit" class="btn btn-primary">Add Credential</button>
</div>`,
		}, nil
	}

	return sdk.AuthFlowState{
		Credentials: map[string]string{"api_key": apiKey},
	}, nil
}
