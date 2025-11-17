package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/tokenusage"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/resp"
)

const (
	pluginName = "ai-quota-per-model"
)

type ChatMode string

const (
	ChatModeCompletion ChatMode = "completion"
	ChatModeAdmin      ChatMode = "admin"
	ChatModeNone       ChatMode = "none"
)

type AdminMode string

const (
	AdminModeRefresh AdminMode = "refresh"
	AdminModeQuery   AdminMode = "query"
	AdminModeDelta   AdminMode = "delta"
	AdminModeNone    AdminMode = "none"
)

func main() {}

func init() {
	wrapper.SetCtx(
		pluginName,
		wrapper.ParseConfig(parseConfig),
		wrapper.ProcessRequestHeaders(onHttpRequestHeaders),
		wrapper.ProcessRequestBody(onHttpRequestBody),
		wrapper.ProcessStreamingResponseBody(onHttpStreamingResponseBody),
	)
}

type QuotaConfig struct {
	redisInfo       RedisInfo `yaml:"redis"`
	RedisKeyPrefix  string    `yaml:"redis_key_prefix"`
	AdminConsumer   string    `yaml:"admin_consumer"`
	AdminPath       string    `yaml:"admin_path"`
	redisClient     wrapper.RedisClient
}

type RedisInfo struct {
	ServiceName string `required:"true" yaml:"service_name" json:"service_name"`
	ServicePort int    `required:"false" yaml:"service_port" json:"service_port"`
	Username    string `required:"false" yaml:"username" json:"username"`
	Password    string `required:"false" yaml:"password" json:"password"`
	Timeout     int    `required:"false" yaml:"timeout" json:"timeout"`
	Database    int    `required:"false" yaml:"database" json:"database"`
}

func parseConfig(json gjson.Result, config *QuotaConfig) error {
	log.Debugf("parse config()")
	// admin
	config.AdminPath = json.Get("admin_path").String()
	config.AdminConsumer = json.Get("admin_consumer").String()
	if config.AdminPath == "" {
		config.AdminPath = "/quota"
	}
	if config.AdminConsumer == "" {
		return errors.New("missing admin_consumer in config")
	}
	// Redis
	config.RedisKeyPrefix = json.Get("redis_key_prefix").String()
	if config.RedisKeyPrefix == "" {
		config.RedisKeyPrefix = "ai_model_quota:"
	}
	redisConfig := json.Get("redis")
	if !redisConfig.Exists() {
		return errors.New("missing redis in config")
	}
	serviceName := redisConfig.Get("service_name").String()
	if serviceName == "" {
		return errors.New("redis service name must not be empty")
	}
	servicePort := int(redisConfig.Get("service_port").Int())
	if servicePort == 0 {
		if strings.HasSuffix(serviceName, ".static") {
			servicePort = 80
		} else {
			servicePort = 6379
		}
	}
	username := redisConfig.Get("username").String()
	password := redisConfig.Get("password").String()
	timeout := int(redisConfig.Get("timeout").Int())
	if timeout == 0 {
		timeout = 1000
	}
	database := int(redisConfig.Get("database").Int())
	config.redisInfo.ServiceName = serviceName
	config.redisInfo.ServicePort = servicePort
	config.redisInfo.Username = username
	config.redisInfo.Password = password
	config.redisInfo.Timeout = timeout
	config.redisInfo.Database = database
	config.redisClient = wrapper.NewRedisClusterClient(wrapper.FQDNCluster{
		FQDN: serviceName,
		Port: int64(servicePort),
	})

	return config.redisClient.Init(username, password, int64(timeout), wrapper.WithDataBase(database))
}

func onHttpRequestHeaders(context wrapper.HttpContext, config QuotaConfig) types.Action {
	context.DisableReroute()
	log.Debugf("onHttpRequestHeaders()")
	// get consumer
	consumer, err := proxywasm.GetHttpRequestHeader("x-mse-consumer")
	if err != nil {
		return deniedNoKeyAuthData()
	}
	if consumer == "" {
		return deniedUnauthorizedConsumer()
	}

	rawPath := context.Path()
	path, _ := url.Parse(rawPath)
	chatMode, adminMode := getOperationMode(path.Path, config.AdminPath)
	context.SetContext("chatMode", chatMode)
	context.SetContext("adminMode", adminMode)
	context.SetContext("consumer", consumer)
	log.Debugf("chatMode:%s, adminMode:%s, consumer:%s", chatMode, adminMode, consumer)

	if chatMode == ChatModeNone {
		return types.ActionContinue
	}

	if chatMode == ChatModeAdmin {
		// query quota
		if adminMode == AdminModeQuery {
			return queryQuota(context, config, consumer, path)
		}
		if adminMode == AdminModeRefresh || adminMode == AdminModeDelta {
			context.BufferRequestBody()
			return types.HeaderStopIteration
		}
		return types.ActionContinue
	}

	// For chat completion mode, buffer request body to extract model
	context.BufferRequestBody()
	return types.HeaderStopIteration
}

func onHttpRequestBody(ctx wrapper.HttpContext, config QuotaConfig, body []byte) types.Action {
	log.Debugf("onHttpRequestBody()")
	chatMode, ok := ctx.GetContext("chatMode").(ChatMode)
	if !ok {
		return types.ActionContinue
	}

	consumer, ok := ctx.GetContext("consumer").(string)
	if !ok {
		return types.ActionContinue
	}

	// Handle admin mode
	if chatMode == ChatModeAdmin {
		adminMode, ok := ctx.GetContext("adminMode").(AdminMode)
		if !ok {
			return types.ActionContinue
		}
		if adminMode == AdminModeRefresh {
			return refreshQuota(ctx, config, consumer, string(body))
		}
		if adminMode == AdminModeDelta {
			return deltaQuota(ctx, config, consumer, string(body))
		}
		return types.ActionContinue
	}

	// Handle chat completion mode - extract model from request body
	if chatMode == ChatModeCompletion {
		model := gjson.Get(string(body), "model").String()
		if model == "" {
			sendResponse(http.StatusBadRequest, "ai-quota.invalid_request", "text/plain", "Missing 'model' field in request body")
			return types.ActionContinue
		}
		ctx.SetContext("model", model)
		log.Debugf("extracted model: %s", model)

		// Check quota
		redisKey := buildRedisKey(config.RedisKeyPrefix, consumer, model)
		config.redisClient.Get(redisKey, func(response resp.Value) {
			isDenied := false
			if err := response.Error(); err != nil {
				log.Warnf("redis error when checking quota: %v", err)
				isDenied = true
			}
			if response.IsNull() {
				log.Warnf("quota not found for consumer:%s model:%s", consumer, model)
				isDenied = true
			}
			if response.Integer() <= 0 {
				log.Warnf("no quota left for consumer:%s model:%s quota:%d", consumer, model, response.Integer())
				isDenied = true
			}
			log.Debugf("get consumer:%s model:%s quota:%d isDenied:%t", consumer, model, response.Integer(), isDenied)
			if isDenied {
				sendResponse(http.StatusForbidden, "ai-quota.noquota", "text/plain", fmt.Sprintf("Request denied by ai quota check. No quota left for model '%s'", model))
				return
			}
			proxywasm.ResumeHttpRequest()
		})
		return types.ActionPause
	}

	return types.ActionContinue
}

func onHttpStreamingResponseBody(ctx wrapper.HttpContext, config QuotaConfig, data []byte, endOfStream bool) []byte {
	chatMode, ok := ctx.GetContext("chatMode").(ChatMode)
	if !ok {
		return data
	}
	if chatMode == ChatModeNone || chatMode == ChatModeAdmin {
		return data
	}

	// Extract token usage from response
	if usage := tokenusage.GetTokenUsage(ctx, data); usage.TotalToken > 0 {
		ctx.SetContext(tokenusage.CtxKeyInputToken, usage.InputToken)
		ctx.SetContext(tokenusage.CtxKeyOutputToken, usage.OutputToken)
	}

	// Only process at end of stream
	if !endOfStream {
		return data
	}

	// Verify we have all required data
	if ctx.GetContext(tokenusage.CtxKeyInputToken) == nil ||
	   ctx.GetContext(tokenusage.CtxKeyOutputToken) == nil ||
	   ctx.GetContext("consumer") == nil ||
	   ctx.GetContext("model") == nil {
		log.Warnf("missing required context data for quota deduction")
		return data
	}

	inputToken := ctx.GetContext(tokenusage.CtxKeyInputToken).(int64)
	outputToken := ctx.GetContext(tokenusage.CtxKeyOutputToken).(int64)
	consumer := ctx.GetContext("consumer").(string)
	model := ctx.GetContext("model").(string)
	totalToken := int(inputToken + outputToken)

	redisKey := buildRedisKey(config.RedisKeyPrefix, consumer, model)
	log.Infof("deducting quota - consumer:%s model:%s tokens:%d", consumer, model, totalToken)
	config.redisClient.DecrBy(redisKey, totalToken, func(response resp.Value) {
		if err := response.Error(); err != nil {
			log.Errorf("redis error when deducting quota: %v", err)
			return
		}
		log.Debugf("quota deducted successfully, remaining: %d", response.Integer())
	})

	return data
}

func buildRedisKey(prefix, consumer, model string) string {
	return prefix + consumer + ":" + model
}

func deniedNoKeyAuthData() types.Action {
	sendResponse(http.StatusUnauthorized, "ai-quota.no_key", "text/plain", "Request denied by ai quota check. No Key Authentication information found.")
	return types.ActionContinue
}

func deniedUnauthorizedConsumer() types.Action {
	sendResponse(http.StatusForbidden, "ai-quota.unauthorized", "text/plain", "Request denied by ai quota check. Unauthorized consumer.")
	return types.ActionContinue
}

func getOperationMode(path string, adminPath string) (ChatMode, AdminMode) {
	fullAdminPath := "/v1/chat/completions" + adminPath
	if strings.HasSuffix(path, fullAdminPath+"/refresh") {
		return ChatModeAdmin, AdminModeRefresh
	}
	if strings.HasSuffix(path, fullAdminPath+"/delta") {
		return ChatModeAdmin, AdminModeDelta
	}
	if strings.HasSuffix(path, fullAdminPath) {
		return ChatModeAdmin, AdminModeQuery
	}
	if strings.HasSuffix(path, "/v1/chat/completions") {
		return ChatModeCompletion, AdminModeNone
	}
	return ChatModeNone, AdminModeNone
}

func refreshQuota(ctx wrapper.HttpContext, config QuotaConfig, adminConsumer string, body string) types.Action {
	// check consumer
	if adminConsumer != config.AdminConsumer {
		sendResponse(http.StatusForbidden, "ai-quota.unauthorized", "text/plain", "Request denied by ai quota check. Unauthorized admin consumer.")
		return types.ActionContinue
	}

	queryValues, _ := url.ParseQuery(body)
	values := make(map[string]string, len(queryValues))
	for k, v := range queryValues {
		values[k] = v[0]
	}

	queryConsumer := values["consumer"]
	model := values["model"]
	quota, err := strconv.Atoi(values["quota"])

	if queryConsumer == "" || model == "" || err != nil {
		sendResponse(http.StatusBadRequest, "ai-quota.invalid_params", "text/plain", "consumer, model and quota (integer) are required")
		return types.ActionContinue
	}

	redisKey := buildRedisKey(config.RedisKeyPrefix, queryConsumer, model)
	err2 := config.redisClient.Set(redisKey, quota, func(response resp.Value) {
		log.Infof("Redis set key=%s quota=%d", redisKey, quota)
		if err := response.Error(); err != nil {
			sendResponse(http.StatusServiceUnavailable, "ai-quota.error", "text/plain", fmt.Sprintf("redis error: %v", err))
			return
		}
		sendResponse(http.StatusOK, "ai-quota.refreshquota", "application/json",
			fmt.Sprintf(`{"consumer":"%s","model":"%s","quota":%d,"status":"refreshed"}`, queryConsumer, model, quota))
	})

	if err2 != nil {
		sendResponse(http.StatusServiceUnavailable, "ai-quota.error", "text/plain", fmt.Sprintf("redis error: %v", err2))
		return types.ActionContinue
	}

	return types.ActionPause
}

func queryQuota(ctx wrapper.HttpContext, config QuotaConfig, adminConsumer string, url *url.URL) types.Action {
	// check consumer
	if adminConsumer != config.AdminConsumer {
		sendResponse(http.StatusForbidden, "ai-quota.unauthorized", "text/plain", "Request denied by ai quota check. Unauthorized admin consumer.")
		return types.ActionContinue
	}

	// check url
	queryValues := url.Query()
	values := make(map[string]string, len(queryValues))
	for k, v := range queryValues {
		values[k] = v[0]
	}

	queryConsumer := values["consumer"]
	model := values["model"]

	if queryConsumer == "" || model == "" {
		sendResponse(http.StatusBadRequest, "ai-quota.invalid_params", "text/plain", "consumer and model parameters are required")
		return types.ActionContinue
	}

	redisKey := buildRedisKey(config.RedisKeyPrefix, queryConsumer, model)
	err := config.redisClient.Get(redisKey, func(response resp.Value) {
		quota := 0
		if err := response.Error(); err != nil {
			sendResponse(http.StatusServiceUnavailable, "ai-quota.error", "text/plain", fmt.Sprintf("redis error: %v", err))
			return
		} else if response.IsNull() {
			quota = 0
		} else {
			quota = response.Integer()
		}

		result := struct {
			Consumer string `json:"consumer"`
			Model    string `json:"model"`
			Quota    int    `json:"quota"`
		}{
			Consumer: queryConsumer,
			Model:    model,
			Quota:    quota,
		}
		body, _ := json.Marshal(result)
		sendResponse(http.StatusOK, "ai-quota.queryquota", "application/json", string(body))
	})

	if err != nil {
		sendResponse(http.StatusServiceUnavailable, "ai-quota.error", "text/plain", fmt.Sprintf("redis error: %v", err))
		return types.ActionContinue
	}

	return types.ActionPause
}

func deltaQuota(ctx wrapper.HttpContext, config QuotaConfig, adminConsumer string, body string) types.Action {
	// check consumer
	if adminConsumer != config.AdminConsumer {
		sendResponse(http.StatusForbidden, "ai-quota.unauthorized", "text/plain", "Request denied by ai quota check. Unauthorized admin consumer.")
		return types.ActionContinue
	}

	queryValues, _ := url.ParseQuery(body)
	values := make(map[string]string, len(queryValues))
	for k, v := range queryValues {
		values[k] = v[0]
	}

	queryConsumer := values["consumer"]
	model := values["model"]
	value, err := strconv.Atoi(values["value"])

	if queryConsumer == "" || model == "" || err != nil {
		sendResponse(http.StatusBadRequest, "ai-quota.invalid_params", "text/plain", "consumer, model and value (integer) are required")
		return types.ActionContinue
	}

	redisKey := buildRedisKey(config.RedisKeyPrefix, queryConsumer, model)

	if value >= 0 {
		err := config.redisClient.IncrBy(redisKey, value, func(response resp.Value) {
			log.Infof("Redis IncrBy key=%s value=%d", redisKey, value)
			if err := response.Error(); err != nil {
				sendResponse(http.StatusServiceUnavailable, "ai-quota.error", "text/plain", fmt.Sprintf("redis error: %v", err))
				return
			}
			sendResponse(http.StatusOK, "ai-quota.deltaquota", "application/json",
				fmt.Sprintf(`{"consumer":"%s","model":"%s","delta":%d,"new_quota":%d}`, queryConsumer, model, value, response.Integer()))
		})
		if err != nil {
			sendResponse(http.StatusServiceUnavailable, "ai-quota.error", "text/plain", fmt.Sprintf("redis error: %v", err))
			return types.ActionContinue
		}
	} else {
		err := config.redisClient.DecrBy(redisKey, 0-value, func(response resp.Value) {
			log.Infof("Redis DecrBy key=%s value=%d", redisKey, 0-value)
			if err := response.Error(); err != nil {
				sendResponse(http.StatusServiceUnavailable, "ai-quota.error", "text/plain", fmt.Sprintf("redis error: %v", err))
				return
			}
			sendResponse(http.StatusOK, "ai-quota.deltaquota", "application/json",
				fmt.Sprintf(`{"consumer":"%s","model":"%s","delta":%d,"new_quota":%d}`, queryConsumer, model, value, response.Integer()))
		})
		if err != nil {
			sendResponse(http.StatusServiceUnavailable, "ai-quota.error", "text/plain", fmt.Sprintf("redis error: %v", err))
			return types.ActionContinue
		}
	}

	return types.ActionPause
}

func sendResponse(statusCode int, statusCodeDetail, contentType, body string) {
	headers := [][2]string{
		{"content-type", contentType},
	}
	proxywasm.SendHttpResponseWithDetail(uint32(statusCode), statusCodeDetail, headers, []byte(body), -1)
}
