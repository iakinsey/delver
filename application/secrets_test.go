package application

import (
	"context"
	"encoding/json"
	"github.com/iakinsey/delver/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestConfigurationProxyIsVisibleAndOtherSecretsRoundTrip(t *testing.T) {
	s := testSupervisor(t)
	RegisterResource("secret_fixture", func(json.RawMessage) (interface{}, error) { return struct{}{}, nil })
	defer RegisterResource("secret_fixture", nil)
	s.state.Active.Config.HTTPClient.HTTPProxyUrl = "http://user:proxy-password@localhost:8088/"
	s.state.Active.Resources = []config.Resource{{Name: "secret", Type: "secret_fixture", Parameters: json.RawMessage(`{"api_key":"private-key","nested":{"password":"nested-password"},"endpoint":"https://example.test/?token=query-secret"}`)}}
	result := redactConfiguration(operationsState{Active: s.state.Active})
	var err error
	require.NoError(t, err)
	encoded, _ := json.Marshal(result)
	for _, secret := range []string{"private-key", "nested-password", "query-secret"} {
		require.NotContains(t, string(encoded), secret)
	}
	require.Contains(t, string(encoded), "http://user:proxy-password@localhost:8088/")
	envelope := result.(map[string]interface{})
	raw, _ := json.Marshal(envelope["configuration"])
	restored, err := restoreConfiguration(raw, s.state.Active)
	require.NoError(t, err)
	var app config.Application
	require.NoError(t, json.Unmarshal(restored, &app))
	require.Equal(t, s.state.Active.Config.HTTPClient.HTTPProxyUrl, app.Config.HTTPClient.HTTPProxyUrl)
	require.JSONEq(t, string(s.state.Active.Resources[0].Parameters), string(app.Resources[0].Parameters))
	require.NotContains(t, s.safeError("failed http://user:proxy-password@localhost:8088/ private-key nested-password query-secret"), "password")
}
func TestConfigurationAcceptsDurationStringsAndRejectsServiceChanges(t *testing.T) {
	s := testSupervisor(t)
	value := configValue(s.state.Active).(map[string]interface{})
	value["config"].(map[string]interface{})["shutdown_timeout"] = "2s"
	raw, _ := json.Marshal(value)
	app, err := DecodeBytes(raw)
	require.NoError(t, err)
	require.Equal(t, 2*time.Second, app.Config.ShutdownTimeout)
	_, err = s.Handle(context.Background(), "draft", raw)
	require.Error(t, err, "executor has no configuration mutation API")
}
