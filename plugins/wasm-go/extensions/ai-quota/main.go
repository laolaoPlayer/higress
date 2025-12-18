package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/tokenusage"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/resp"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-quota/util"
)

const (
	pluginName = "ai-quota"
)

func main() {}

func init() {
	wrapper.SetCtx(
		pluginName,
		wrapper.ParseConfig(parseConfig),
		wrapper.ProcessRequestHeaders(onHttpRequestHeaders),
		wrapper.ProcessStreamingResponseBody(onHttpStreamingResponseBody),
	)
}

type QuotaConfig struct {
	redisInfo      RedisInfo           `yaml:"redis"`
	RedisKeyPrefix string              `yaml:"redis_key_prefix"`
	redisClient    wrapper.RedisClient
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
	// Redis key prefix
	config.RedisKeyPrefix = json.Get("redis_key_prefix").String()
	if config.RedisKeyPrefix == "" {
		config.RedisKeyPrefix = "chat_quota:"
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
			// use default logic port which is 80 for static service
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

	// Get consumer from authentication plugin
	consumer, err := proxywasm.GetHttpRequestHeader("x-mse-consumer")
	if err != nil {
		return deniedNoKeyAuthData()
	}
	if consumer == "" {
		return deniedUnauthorizedConsumer()
	}

	// Check if this is a chat completion request
	rawPath := context.Path()
	if !strings.HasSuffix(rawPath, "/v1/chat/completions") {
		return types.ActionContinue
	}

	// Store consumer for later use
	context.SetContext("consumer", consumer)
	context.DontReadRequestBody()

	// Check quota from Redis
	config.redisClient.Get(config.RedisKeyPrefix+consumer, func(response resp.Value) {
		isDenied := false
		if err := response.Error(); err != nil {
			isDenied = true
		}
		if response.IsNull() {
			isDenied = true
		}
		if response.Integer() <= 0 {
			isDenied = true
		}
		log.Debugf("get consumer:%s quota:%d isDenied:%t", consumer, response.Integer(), isDenied)
		if isDenied {
			util.SendResponse(http.StatusForbidden, "ai-quota.noquota", "text/plain", "Request denied by ai quota check, No quota left")
			return
		}
		proxywasm.ResumeHttpRequest()
	})
	return types.HeaderStopAllIterationAndWatermark
}

func onHttpStreamingResponseBody(ctx wrapper.HttpContext, config QuotaConfig, data []byte, endOfStream bool) []byte {
	if usage := tokenusage.GetTokenUsage(ctx, data); usage.TotalToken > 0 {
		ctx.SetContext(tokenusage.CtxKeyInputToken, usage.InputToken)
		ctx.SetContext(tokenusage.CtxKeyOutputToken, usage.OutputToken)
	}

	// chat completion mode
	if !endOfStream {
		return data
	}

	if ctx.GetContext(tokenusage.CtxKeyInputToken) == nil || ctx.GetContext(tokenusage.CtxKeyOutputToken) == nil || ctx.GetContext("consumer") == nil {
		return data
	}

	inputToken := ctx.GetContext(tokenusage.CtxKeyInputToken).(int64)
	outputToken := ctx.GetContext(tokenusage.CtxKeyOutputToken).(int64)
	consumer := ctx.GetContext("consumer").(string)
	totalToken := int(inputToken + outputToken)
	log.Debugf("update consumer:%s, totalToken:%d", consumer, totalToken)
	config.redisClient.DecrBy(config.RedisKeyPrefix+consumer, totalToken, nil)
	return data
}

func deniedNoKeyAuthData() types.Action {
	util.SendResponse(http.StatusUnauthorized, "ai-quota.no_key", "text/plain", "Request denied by ai quota check. No Key Authentication information found.")
	return types.ActionContinue
}

func deniedUnauthorizedConsumer() types.Action {
	util.SendResponse(http.StatusForbidden, "ai-quota.unauthorized", "text/plain", "Request denied by ai quota check. Unauthorized consumer.")
	return types.ActionContinue
}
