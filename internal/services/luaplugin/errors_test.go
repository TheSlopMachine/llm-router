package luaplugin

import (
	"errors"
	"testing"
)

func TestHandlerNotFoundSentinel(t *testing.T) {
	notFound := &notFoundError{PluginID: "p", TypeKey: "t", Handler: "auth_initiate"}
	if !errors.Is(notFound, ErrHandlerNotFound) {
		t.Error("notFoundError must unwrap to ErrHandlerNotFound")
	}
	for _, err := range []error{
		nil,
		errors.New("no plugin registered for type key \"custom\""),
		errors.New("boom"),
	} {
		if errors.Is(err, ErrHandlerNotFound) {
			t.Errorf("ErrHandlerNotFound must not match %v", err)
		}
	}
}
