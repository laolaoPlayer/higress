// Copyright (c) 2025 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"testing"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/test"
	"github.com/stretchr/testify/require"
)

// Test configuration: basic config
var basicConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"admin_consumer":   "admin",
		"redis_key_prefix": "ai_model_quota:",
		"admin_path":       "/quota",
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
			"timeout":      1000,
			"database":     0,
		},
	})
	return data
}()

// Test configuration: missing admin_consumer
var missingAdminConsumerConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
		},
	})
	return data
}()

// Test configuration: missing redis
var missingRedisConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"admin_consumer": "admin",
	})
	return data
}()

func TestParseConfig(t *testing.T) {
	test.RunGoTest(t, func(t *testing.T) {
		// Test basic config parsing
		t.Run("basic config", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)
			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)

			quotaConfig := config.(*QuotaConfig)
			require.Equal(t, "admin", quotaConfig.AdminConsumer)
			require.Equal(t, "ai_model_quota:", quotaConfig.RedisKeyPrefix)
			require.Equal(t, "/quota", quotaConfig.AdminPath)
		})

		// Test missing admin_consumer
		t.Run("missing admin_consumer", func(t *testing.T) {
			host, status := test.NewTestHost(missingAdminConsumerConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusFailed, status)
		})

		// Test missing redis config
		t.Run("missing redis", func(t *testing.T) {
			host, status := test.NewTestHost(missingRedisConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusFailed, status)
		})
	})
}

func TestOnHttpRequestHeaders(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// Test chat completion mode request headers
		t.Run("chat completion mode - should buffer body", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// Set request headers with consumer info
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions"},
				{":method", "POST"},
				{"x-mse-consumer", "consumer1"},
			})

			// Should buffer request body to extract model
			require.Equal(t, types.HeaderStopIteration, action)
		})

		// Test missing consumer
		t.Run("missing consumer", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions"},
				{":method", "POST"},
			})

			// Should deny with 401
			require.Equal(t, types.ActionContinue, action)
			localResponse := host.GetLocalResponse()
			require.Equal(t, uint32(401), localResponse.StatusCode)
		})

		// Test admin mode - query
		t.Run("admin mode - query", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions/quota?consumer=consumer1&model=gpt-4"},
				{":method", "GET"},
				{"x-mse-consumer", "admin"},
			})

			// Should pause for Redis query
			require.Equal(t, types.ActionPause, action)
		})

		// Test admin mode - refresh
		t.Run("admin mode - refresh", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions/quota/refresh"},
				{":method", "POST"},
				{"x-mse-consumer", "admin"},
			})

			// Should buffer body for processing
			require.Equal(t, types.HeaderStopIteration, action)
		})
	})
}

func TestOnHttpRequestBody(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// Test chat completion mode with valid model
		t.Run("chat completion with valid model", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// First set request headers
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions"},
				{":method", "POST"},
				{"x-mse-consumer", "consumer1"},
			})

			// Set request body with model field
			body := `{"model": "gpt-4", "messages": [{"role": "user", "content": "Hello"}]}`
			action := host.CallOnHttpRequestBody([]byte(body))

			// Should pause for Redis quota check
			require.Equal(t, types.ActionPause, action)

			// Simulate Redis response (sufficient quota)
			resp := test.CreateRedisResp(1000)
			host.PutRedisResp(resp)

			// Should resume request
			localResponse := host.GetLocalResponse()
			require.Nil(t, localResponse)
		})

		// Test chat completion mode with missing model
		t.Run("chat completion with missing model", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// First set request headers
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions"},
				{":method", "POST"},
				{"x-mse-consumer", "consumer1"},
			})

			// Set request body without model field
			body := `{"messages": [{"role": "user", "content": "Hello"}]}`
			action := host.CallOnHttpRequestBody([]byte(body))

			// Should return error
			require.Equal(t, types.ActionContinue, action)
			localResponse := host.GetLocalResponse()
			require.Equal(t, uint32(400), localResponse.StatusCode)
		})

		// Test admin mode - refresh quota
		t.Run("admin refresh quota", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// First set request headers
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions/quota/refresh"},
				{":method", "POST"},
				{"x-mse-consumer", "admin"},
			})

			// Set refresh body
			body := "consumer=consumer1&model=gpt-4&quota=10000"
			action := host.CallOnHttpRequestBody([]byte(body))

			// Should pause for Redis operation
			require.Equal(t, types.ActionPause, action)

			// Simulate Redis OK response
			resp := test.CreateRedisResp("OK")
			host.PutRedisResp(resp)

			// Should return success response
			localResponse := host.GetLocalResponse()
			require.Equal(t, uint32(200), localResponse.StatusCode)
		})

		// Test admin mode - delta quota
		t.Run("admin delta quota", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// First set request headers
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions/quota/delta"},
				{":method", "POST"},
				{"x-mse-consumer", "admin"},
			})

			// Set delta body (increase by 1000)
			body := "consumer=consumer1&model=gpt-4&value=1000"
			action := host.CallOnHttpRequestBody([]byte(body))

			// Should pause for Redis operation
			require.Equal(t, types.ActionPause, action)

			// Simulate Redis response (new value)
			resp := test.CreateRedisResp(11000)
			host.PutRedisResp(resp)

			// Should return success response
			localResponse := host.GetLocalResponse()
			require.Equal(t, uint32(200), localResponse.StatusCode)
		})

		// Test admin unauthorized
		t.Run("admin unauthorized", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// Set request headers with non-admin consumer
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions/quota/refresh"},
				{":method", "POST"},
				{"x-mse-consumer", "consumer1"},
			})

			// Try to refresh quota
			body := "consumer=consumer1&model=gpt-4&quota=10000"
			action := host.CallOnHttpRequestBody([]byte(body))

			// Should deny with 403
			require.Equal(t, types.ActionContinue, action)
			localResponse := host.GetLocalResponse()
			require.Equal(t, uint32(403), localResponse.StatusCode)
		})
	})
}

func TestBuildRedisKey(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		consumer string
		model    string
		expected string
	}{
		{
			name:     "basic key",
			prefix:   "ai_model_quota:",
			consumer: "consumer1",
			model:    "gpt-4",
			expected: "ai_model_quota:consumer1:gpt-4",
		},
		{
			name:     "different model",
			prefix:   "ai_model_quota:",
			consumer: "consumer1",
			model:    "gpt-3.5-turbo",
			expected: "ai_model_quota:consumer1:gpt-3.5-turbo",
		},
		{
			name:     "custom prefix",
			prefix:   "custom:",
			consumer: "user1",
			model:    "claude-3",
			expected: "custom:user1:claude-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRedisKey(tt.prefix, tt.consumer, tt.model)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestGetOperationMode(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		adminPath      string
		expectedChat   ChatMode
		expectedAdmin  AdminMode
	}{
		{
			name:          "chat completion",
			path:          "/v1/chat/completions",
			adminPath:     "/quota",
			expectedChat:  ChatModeCompletion,
			expectedAdmin: AdminModeNone,
		},
		{
			name:          "admin query",
			path:          "/v1/chat/completions/quota",
			adminPath:     "/quota",
			expectedChat:  ChatModeAdmin,
			expectedAdmin: AdminModeQuery,
		},
		{
			name:          "admin refresh",
			path:          "/v1/chat/completions/quota/refresh",
			adminPath:     "/quota",
			expectedChat:  ChatModeAdmin,
			expectedAdmin: AdminModeRefresh,
		},
		{
			name:          "admin delta",
			path:          "/v1/chat/completions/quota/delta",
			adminPath:     "/quota",
			expectedChat:  ChatModeAdmin,
			expectedAdmin: AdminModeDelta,
		},
		{
			name:          "none mode",
			path:          "/v1/other",
			adminPath:     "/quota",
			expectedChat:  ChatModeNone,
			expectedAdmin: AdminModeNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chatMode, adminMode := getOperationMode(tt.path, tt.adminPath)
			require.Equal(t, tt.expectedChat, chatMode)
			require.Equal(t, tt.expectedAdmin, adminMode)
		})
	}
}
