package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	wr "github.com/mroth/weightedrand"
	"github.com/nacos-group/nacos-sdk-go/clients"
	"github.com/nacos-group/nacos-sdk-go/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/common/constant"
	"github.com/nacos-group/nacos-sdk-go/model"
	"github.com/nacos-group/nacos-sdk-go/vo"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

var rwLock *sync.RWMutex                             //读写锁
var client naming_client.INamingClient               //nacos客户端
var ServiceRoute map[string][]model.SubscribeService //本地服务缓存

func main() {
	rand.Seed(time.Now().UTC().UnixNano())
	ServiceRoute = make(map[string][]model.SubscribeService)
	rwLock = new(sync.RWMutex)

	//初始化Nacos配置相关信息
	initDiscovery()

	//订阅服务
	subServices()

	gin.SetMode(gin.ReleaseMode)
	route := gin.New()
	route.Use(func(context *gin.Context) {

		beginTime := time.Now().UnixNano()

		Middleware(context.Writer, context.Request)

		endTime := time.Now().UnixNano()

		fmt.Println(endTime - beginTime)
	})

	route.Run(":5050")
}

func Middleware(response http.ResponseWriter, request *http.Request) {
	//获取url 即 问号之前那部分
	urlString := request.RequestURI
	if strings.Index(urlString, "?") > 0 {
		urlString = urlString[0:strings.Index(urlString, "?")]
	}

	//如果是默认根目录，则返回204
	if urlString == "/" {
		response.WriteHeader(204)
		return
	}

	//取url串的第一节的小写字母为服务名 如/A/b/c  则a为当前要请求的服务名
	serviceName := strings.ToLower(strings.Split(urlString, "/")[1])

	//如果不存在，则返回404
	if _, ok := ServiceRoute[serviceName]; !ok {
		response.WriteHeader(404)
		return
	}

	//按权重填充，并随机抽取一个值
	var choices []wr.Choice
	rwLock.RLock()
	for _, instance := range ServiceRoute[serviceName] {
		c := wr.Choice{Item: fmt.Sprintf("http://%s:%d", instance.Ip, instance.Port), Weight: uint(instance.Weight)}
		choices = append(choices, c)
	}
	rwLock.RUnlock()
	chooser, _ := wr.NewChooser(choices...)
	targetHost := chooser.Pick().(string)

	targetHost = "http://127.0.0.1:5151"
	remote, err := url.Parse(targetHost)

	if err != nil {
		panic(err)
	}

	proxy := httputil.NewSingleHostReverseProxy(remote)
	proxy.ServeHTTP(response, request)
}

func initDiscovery() {
	sc := []constant.ServerConfig{
		{
			IpAddr: "mse-e52dbdd6-p.nacos-ans.mse.aliyuncs.com",
			Port:   8848,
			Scheme: "http",
		},
	}

	cc := constant.ClientConfig{
		TimeoutMs:   500,
		NamespaceId: "27fdefc2-ae39-41fd-bac4-9256acbf97bc",
		//CacheDir:             "e:/nacos/cache",
		NotLoadCacheAtStart:  true,
		UpdateCacheWhenEmpty: false,
		//LogDir:               "e:/nacos/log",
	}

	client, _ = clients.CreateNamingClient(map[string]interface{}{
		"serverConfigs": sc,
		"clientConfig":  cc,
	})
}

func subServices() {
	//从nacos上拉取注册的服务列表
	services, _ := client.GetAllServicesInfo(vo.GetAllServiceInfoParam{
		PageNo:   1,
		PageSize: 9999,
	})

	//对列表中的每一个服务都订阅
	for _, v := range services.Doms {
		client.Subscribe(&vo.SubscribeParam{
			ServiceName:       v,
			SubscribeCallback: changeNotity,
		})
	}
}

func changeNotity(subInstances []model.SubscribeService, err error) {
	//不处理空推送
	if len(subInstances) == 0 {
		return
	}

	//遍历订阅服务列表
	for _, subInstance := range subInstances {
		//判断当前服务是否存在本地服务路由表中，如果不在则加入进去
		if _, ok := ServiceRoute[subInstance.ServiceName]; !ok {
			ServiceRoute[subInstance.ServiceName] = append(ServiceRoute[subInstance.ServiceName], subInstance)
			continue
		}

		//当前 根据服务名、IP、端口 确定该服务是否已登记，后续可考虑加入版本号
		hasThisInstance := false
		currentInstanceIndex := -1
		for i, currentLocalInstance := range ServiceRoute[subInstance.ServiceName] {
			if currentLocalInstance.ServiceName == subInstance.ServiceName && currentLocalInstance.Ip == subInstance.Ip && currentLocalInstance.Port == subInstance.Port {
				hasThisInstance = true
				currentInstanceIndex = i
				break
			}
		}
		//如果当前实例不存在就登记进来
		if !hasThisInstance && subInstance.Valid == true {
			ServiceRoute[subInstance.ServiceName] = append(ServiceRoute[subInstance.ServiceName], subInstance)
		}
		//如果已存在，且推送的状态为Valid，则移除掉
		if hasThisInstance && subInstance.Valid == false {
			beforeList := ServiceRoute[subInstance.ServiceName][:currentInstanceIndex]
			afterList := ServiceRoute[subInstance.ServiceName][currentInstanceIndex+1:]
			rwLock.Lock()
			ServiceRoute[subInstance.ServiceName] = append(beforeList, afterList...)
			rwLock.Unlock()
		}
	}
}
