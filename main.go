package main

import (
	"fmt"
	wr "github.com/mroth/weightedrand"
	"github.com/nacos-group/nacos-sdk-go/clients"
	"github.com/nacos-group/nacos-sdk-go/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/common/constant"
	"github.com/nacos-group/nacos-sdk-go/vo"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

var client naming_client.INamingClient

func main() {

	rand.Seed(time.Now().UTC().UnixNano())

	sc := []constant.ServerConfig{
		{
			IpAddr: "mse-e52dbdd6-p.nacos-ans.mse.aliyuncs.com",
			Port:   8848,
			Scheme: "http",
		},
	}

	cc := constant.ClientConfig{
		TimeoutMs:   500,
		NamespaceId: "c0d565da-10ef-40a4-84b2-0b19dc25c4db",
		//CacheDir:             "e:/nacos/cache",
		NotLoadCacheAtStart:  true,
		UpdateCacheWhenEmpty: false,
		//LogDir:               "e:/nacos/log",
	}

	client, _ = clients.CreateNamingClient(map[string]interface{}{
		"serverConfigs": sc,
		"clientConfig":  cc,
	})

	http.HandleFunc("/", process)
	http.ListenAndServe(":5050", nil)
}

func process(w http.ResponseWriter, r *http.Request) {
	//获取url 即 问号之前那部分
	urlString := r.RequestURI
	if strings.Index(urlString, "?") > 0 {
		urlString = urlString[0:strings.Index(urlString, "?")]
	}

	//如果是默认根目录，则返回204
	if urlString == "/" {
		w.WriteHeader(204)
		return
	}

	//取url串的第一节的小写字母为服务名 如/A/b/c  则a为当前要请求的服务名
	serviceName := strings.ToLower(strings.Split(urlString, "/")[1])

	services, err := client.GetAllServicesInfo(vo.GetAllServiceInfoParam{
		NameSpace: "c0d565da-10ef-40a4-84b2-0b19dc25c4db",
		PageNo:    1,
		PageSize:  999,
	})

	//判断当前请求的服务是否在nacos的服务列表里
	isExistService := false
	for _, v := range services.Doms {
		if strings.ToLower(v) == serviceName {
			isExistService = true
			break
		}
	}

	//如果不存在，则返回404
	if !isExistService {
		w.WriteHeader(404)
		return
	}

	//获取该服务下的实例列表
	instances, err := client.SelectAllInstances(vo.SelectAllInstancesParam{
		ServiceName: serviceName,
		GroupName:   "DEFAULT_GROUP",
	})

	var choices []wr.Choice
	for _, v := range instances {
		c := wr.Choice{Item: fmt.Sprintf("http://%s:%d", v.Ip, v.Port), Weight: uint(v.Weight) * 5}
		choices = append(choices, c)
	}
	chooser, _ := wr.NewChooser(choices...)
	targetHost := chooser.Pick().(string)

	//fmt.Println(targetHost)

	targetHost = "http://127.0.0.1:5151"

	remote, err := url.Parse(targetHost)

	if err != nil {
		panic(err)
	}

	proxy := httputil.NewSingleHostReverseProxy(remote)

	proxy.ServeHTTP(w, r)
}
