package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var configFilePath = "/etc/v2fly/"
var configFileName = "config.json"
var proxyHttpPort = 1080
var proxySocksPort = 1081

func main() {
	// 1. 使用 http 获取 subscribeUrl 的 base64 编码内容
	url := flag.String("url", "default name", "a http url to subscribe")
	flag.Parse()
	if url == nil {
		fmt.Println("param '-url' can't be null!")
		return
	}
	fmt.Println("subscribe url ：" + *url)
	var server = ""
	ticker := time.NewTicker(time.Second * 1)
	for range ticker.C {
		result, _ := parseURL(*url)
		fastestServer, _ := selectFastest(result)
		fastestServerStr := fastestServer["add"].(string) + ":" + fastestServer["port"].(string)
		if server == fastestServerStr {
			continue
		}
		server = fastestServerStr
		marshal, _ := json.Marshal(fastestServer)
		fmt.Println(string(marshal))
		reloadConfigFile(fastestServer)
	}
}

func parseURL(url string) ([]map[string]interface{}, error) {
	if strings.HasPrefix(url, "ss://") {
		// 解析 SS 协议 URL 的相关信息
		ssParts := strings.Split(url[5:], "#")
		ssMethodAndPassword, err := base64.RawStdEncoding.DecodeString(ssParts[0])
		if err != nil {
			return nil, err
		}
		ssMethodAndPasswordParts := strings.Split(string(ssMethodAndPassword), ":")
		method := ssMethodAndPasswordParts[0]
		passAndAddr := strings.Split(ssMethodAndPasswordParts[1], "@")
		password := passAndAddr[0]
		addr := passAndAddr[1]
		port := ssMethodAndPasswordParts[2]

		// 构造 JSON 对象并返回
		obj := map[string]interface{}{
			"type":     "ss",
			"add":      addr,
			"port":     port,
			"method":   method,
			"password": password,
			"outboundsText": map[string]interface{}{
				"tag":      "proxy",
				"protocol": "shadowsocks",
				"settings": map[string]interface{}{
					"servers": map[string]interface{}{
						"address":  addr,
						"method":   method,
						"ota":      false,
						"password": password,
						"port":     port,
						"level":    1,
					},
				},
			},
		}
		return []map[string]interface{}{obj}, nil
	} else if strings.HasPrefix(url, "vmess://") {
		// 解析 Vmess 协议 URL 的相关信息
		vmessParts, err := base64.RawStdEncoding.DecodeString(url[8:])
		if err != nil {
			return nil, err
		}

		// 将 JSON 字符串解析为 map[string]interface{}
		var obj map[string]interface{}
		err = json.Unmarshal(vmessParts, &obj)
		if err != nil {
			return nil, err
		}
		obj["type"] = "vmess"
		port, err := strconv.Atoi(obj["port"].(string))
		obj["v2rayText"] = map[string]interface{}{
			"tag":      "proxy",
			"protocol": "vmess",
			"settings": map[string]interface{}{
				"vnext": []map[string]interface{}{
					{
						"address": obj["add"],
						"port":    port,
						"users": []map[string]interface{}{
							{
								"id":       obj["id"],
								"alterId":  obj["aid"],
								"email":    "t@t.tt",
								"security": "auto",
							},
						},
					},
				},
			},
		}
		return []map[string]interface{}{obj}, nil
	} else if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}
		content := string(body)

		// 解码并获取协议链接
		decodedContent, err := base64.StdEncoding.DecodeString(content)
		urls := strings.Split(string(decodedContent), "\n")
		var result []map[string]interface{}
		for _, urlz := range urls {
			obj, err := parseURL(urlz)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			result = append(result, obj...)
		}

		// 将结果以 JSON 数组的形式返回
		return result, nil
	}

	// 如果 URL 不是 SS、Vmess 协议或 http/https，则返回错误
	return nil, errors.New("unsupported URL protocol")
}

func selectFastest(servers []map[string]interface{}) (map[string]interface{}, error) {
	var fastestServer map[string]interface{}
	var minLatency float64
	for _, server := range servers {
		ip := server["add"].(string)
		port := server["port"].(string)

		// 获取当前时间戳
		start := time.Now().UnixNano()

		// 建立 TCP 连接
		conn, err := net.Dial("tcp", ip+":"+port)
		if err != nil {
			fmt.Println(ip + ":" + port + " is not available")
			continue
		}
		defer conn.Close()

		// 计算延迟
		end := time.Now().UnixNano()
		latency := float64(end-start) / float64(time.Millisecond)

		//fmt.Printf("%s:%s is available, latency: %.2f ms\n", ip, port, latency)

		// 更新最小延迟和最快的服务器
		if fastestServer == nil || latency < minLatency {
			fastestServer = server
			minLatency = latency
		}
	}

	if fastestServer == nil {
		return nil, errors.New("no available server found")
	}

	return fastestServer, nil
}

func getConfigTemplate() map[string]interface{} {
	return map[string]interface{}{
		"log": map[string]interface{}{
			"access":   "",
			"error":    "",
			"loglevel": "debug",
		},
		"inbounds": []map[string]interface{}{
			{
				"tag":      "socks",
				"port":     proxySocksPort,
				"listen":   "0.0.0.0",
				"protocol": "socks",
				"sniffing": map[string]interface{}{
					"enabled":      false,
					"destOverride": []interface{}{"http", "tls"},
					"routeOnly":    false,
				},
				"settings": map[string]interface{}{
					"auth":             "noauth",
					"udp":              true,
					"allowTransparent": false,
				},
			},
			{
				"tag":      "http",
				"port":     proxyHttpPort,
				"listen":   "0.0.0.0",
				"protocol": "http",
				"sniffing": map[string]interface{}{
					"enabled":      false,
					"destOverride": []interface{}{"http", "tls"},
					"routeOnly":    false,
				},
				"settings": map[string]interface{}{
					"auth":             "noauth",
					"udp":              true,
					"allowTransparent": false,
				},
			},
		},
		"outbounds": []map[string]interface{}{
			{
				"tag":      "direct",
				"protocol": "freedom",
				"settings": map[string]interface{}{},
			},
			{
				"tag":      "block",
				"protocol": "blackhole",
				"settings": map[string]interface{}{
					"response": map[string]interface{}{
						"type": "http",
					},
				},
			},
		},
		"routing": map[string]interface{}{
			"domainStrategy": "AsIs",
			"rules": []interface{}{
				map[string]interface{}{
					"id":          "5670512747724795459",
					"type":        "field",
					"port":        "0-65535",
					"outboundTag": "proxy",
					"enabled":     true,
				},
			},
		},
	}
}

func reloadConfigFile(server map[string]interface{}) {
	date := getConfigTemplate()
	date["outbounds"] = append(date["outbounds"].([]map[string]interface{}), server["v2rayText"].(map[string]interface{}))
	jsonData, err := json.MarshalIndent(date, "", "    ")
	// 将美化过的 JSON 数据输出到 config.json 文件
	errmk := os.MkdirAll(configFilePath, os.ModePerm)
	if errmk != nil {
		panic(errmk)
	}
	err = ioutil.WriteFile(configFilePath+configFileName, jsonData, 0644)
	if err != nil {
		panic(err)
	}
	restartV2Ray()
}

func restartV2Ray() {
	killCmd := exec.Command("pkill", "v2ray")
	if err := killCmd.Run(); err != nil {
		fmt.Println("failed to kill v2ray:", err)
	}
	cmd := exec.Command("v2ray", "run", "-c", configFilePath+configFileName)
	err := cmd.Start()
	if err != nil {
		fmt.Println("failed to restart v2ray:", err)
	}
}
