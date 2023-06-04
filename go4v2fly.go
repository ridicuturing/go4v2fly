package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var interval = flag.Int("interval", 10, "a http url to subscribe")
var subscribeUrl = flag.String("url", "", "a http url to subscribe")
var configFilePath = "/etc/v2fly/"
var configFileName = "config.json"
var proxyHttpPort = 1080
var proxySocksPort = 1081

const (
	STATE_READY_FOR_SUBSCRIBE = iota
	STATE_SUBSCRIPTION_NOT_UPDATE
	STATE_FIND_FASTEST_PROXY
	STATE_SWITCH_TO_CURRENT_FASTEST_PROXY
	STATE_CHECK
	STATE_TEMPORARY_ANOMALY
	STATE_DONE
	STATE_ERROR
	STATE_EXIT
)

func main() {
	flag.Parse()
	fmt.Println("subscribe url ：" + *subscribeUrl)
	state := STATE_READY_FOR_SUBSCRIBE
	for {
		state = stateProcessor(state)
	}
}

func stateProcessor(state int) (nextState int) {
	switch state {
	case STATE_READY_FOR_SUBSCRIBE:
		return pullProxiesFromUrl()
	case STATE_FIND_FASTEST_PROXY:
		return findFastestProxy()
	case STATE_SWITCH_TO_CURRENT_FASTEST_PROXY:
		return switchToCurrentFastestProxy()
	case STATE_SUBSCRIPTION_NOT_UPDATE:
	case STATE_CHECK:
		return checkCurrentProxy()
	case STATE_DONE:
	case STATE_ERROR:
	case STATE_TEMPORARY_ANOMALY:
	default:
		return goSleep()
	}
	return goSleep()
}

func goSleep() int {
	time.Sleep(time.Second * time.Duration(*interval))
	return STATE_READY_FOR_SUBSCRIBE
}

func checkCurrentProxy() int {
	for i := 0; i < 5; i++ {
		fmt.Println("try go connet openai with proxy : " + currentFastestProxy["pid"].(string))
		if checkOpenaiAvailable() {
			return STATE_DONE
		}
	}
	fmt.Println("remove proxy : " + currentFastestProxy["pid"].(string))
	delete(currentProxies, currentFastestProxy["pid"].(string))
	currentFastestProxy = nil
	return STATE_FIND_FASTEST_PROXY
}

func checkOpenaiAvailable() bool {
	proxyURL, _ := url.Parse("http://127.0.0.1:1080")
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	client := &http.Client{Transport: transport}

	resp, err := client.Get("https://api.openai.com")
	if err != nil {
		fmt.Println(err)
		return false
	}
	// 判断网络是否联通
	defer resp.Body.Close()

	// 读取 Response Body 并将其打印到控制台
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return false
	}
	if resp.StatusCode >= 0 {
		fmt.Println(string(body))
		return true
	}
	return false
}

func switchToCurrentFastestProxy() int {
	fmt.Println("switch to " + currentFastestProxy["pid"].(string))
	reloadConfigFile(currentFastestProxy)
	return STATE_CHECK
}
func reloadConfigFile(server map[string]interface{}) {
	date := getConfigTemplate()
	date["outbounds"] = append(date["outbounds"].([]map[string]interface{}), server["v2rayText"].(map[string]interface{}))
	jsonData, err := json.MarshalIndent(date, "", "    ")
	// 将美化过的 JSON 数据输出到 config.json 文件
	errmk := os.MkdirAll(configFilePath, os.ModePerm)
	if errmk != nil {
		fmt.Println(errmk)
	}
	err = ioutil.WriteFile(configFilePath+configFileName, jsonData, 0644)
	if err != nil {
		fmt.Println(err)
	}
	startV2Ray()
}

func startV2Ray() {
	if runtime.GOOS == "windows" {
		killCmd := exec.Command("taskkill", "/IM", "v2ray.exe", "/F")
		err := killCmd.Run()
		if err != nil {
			fmt.Println("failed to terminate v2ray:", err)
		}
	} else {
		killCmd := exec.Command("pkill", "v2ray")
		if err := killCmd.Run(); err != nil {
			fmt.Println("failed to pkill v2ray:", err)
		}
	}

	cmd := exec.Command("v2ray", "run", "-c", configFilePath+configFileName)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("Error obtaining stdout pipe:", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		fmt.Println("Error obtaining stderr pipe:", err)
	}
	// 启动命令
	err = cmd.Start()
	go func() {
		scannerOut := bufio.NewScanner(stdout)
		for scannerOut.Scan() {
			fmt.Println(scannerOut.Text())
		}
	}()
	go func() {
		scannerErr := bufio.NewScanner(stderr)
		for scannerErr.Scan() {
			fmt.Println(scannerErr.Text())
		}
	}()
	time.Sleep(time.Second * 1)
}

var currentFastestProxy map[string]interface{}

func findFastestProxy() int {
	type result struct {
		proxy   map[string]interface{}
		latency float64
		err     error
	}
	resultChan := make(chan result, len(currentProxies))

	for _, proxy := range currentProxies {
		go func(proxy map[string]interface{}) {
			ip := proxy["add"].(string)
			port := strconv.Itoa(proxy["port"].(int))

			// 获取当前时间戳
			start := time.Now().UnixNano()

			// 建立 TCP 连接
			dialer := &net.Dialer{
				Timeout: 10 * time.Second,
			}
			conn, err := dialer.Dial("tcp", ip+":"+port)
			if err != nil {
				resultChan <- result{proxy, 0, err}
				return
			}
			defer conn.Close()

			// 计算延迟
			end := time.Now().UnixNano()
			latency := float64(end-start) / float64(time.Millisecond)

			resultChan <- result{proxy, latency, nil}
		}(proxy.(map[string]interface{}))
	}

	var newFastestProxy map[string]interface{}
	var minLatency float64
	for i := 0; i < len(currentProxies); i++ {
		r := <-resultChan
		if r.err != nil || r.latency == 0 {
			currentProxies[r.proxy["pid"].(string)] = nil
			// 处理连接错误
			continue
		}

		// 更新最小延迟和最快的服务器
		if newFastestProxy == nil || r.latency < minLatency {
			newFastestProxy = r.proxy
			minLatency = r.latency
		}
	}

	if newFastestProxy != nil {
		currentFastestProxy = newFastestProxy
		return STATE_SWITCH_TO_CURRENT_FASTEST_PROXY
	} else {
		if currentFastestProxy == nil {
			fmt.Println("no available proxy")
			return STATE_TEMPORARY_ANOMALY
		} else {
			return STATE_CHECK
		}
	}
}

var currentProxies = make(map[string]interface{})
var pullProxies = make(map[string]interface{})

func pullProxiesFromUrl() (nextState int) {
	if subscribeUrl == nil {
		fmt.Println("param '-url' can't be null!")
		return STATE_ERROR
	}
	var err error
	pullProxies, err = parseURLToMap(*subscribeUrl)
	if err != nil && err.Error() == "STATE_SUBSCRIPTION_NOT_UPDATE" {
		return STATE_SUBSCRIPTION_NOT_UPDATE
	}
	for key, value := range pullProxies {
		currentProxies[key] = value
	}
	return STATE_FIND_FASTEST_PROXY
}

func parseURLToMap(url string) (map[string]interface{}, error) {
	switch {
	case strings.HasPrefix(url, "ss://"):
		return parseSSToMap(url)
	case strings.HasPrefix(url, "vmess://"):
		return parseVmessToMap(url)
	case strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://"):
		return parseHTTPToMap(url)
	default:
		return nil, errors.New("unsupported URL protocol(" + url + ")")
	}
}

func parseSSToMap(url string) (map[string]interface{}, error) {
	// 解析 SS 协议 URL 的相关信息
	ssParts := strings.Split(url[5:], "#")
	ssMethodAndPassword, err := tryBase64Decode(ssParts[0])
	if err != nil {
		return nil, err
	}
	ssMethodAndPasswordParts := strings.Split(ssMethodAndPassword, ":")
	method := ssMethodAndPasswordParts[0]
	passAndAddr := strings.Split(ssMethodAndPasswordParts[1], "@")
	password := passAndAddr[0]
	addr := passAndAddr[1]
	portStr := ssMethodAndPasswordParts[2]
	port, err := strconv.Atoi(portStr)

	// 构造 JSON 对象并返回
	obj := map[string]interface{}{
		"pid":      addr + ":" + portStr,
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
		"v2rayText": map[string]interface{}{
			"tag":      "proxy",
			"protocol": "shadowsocks",
			"settings": map[string]interface{}{
				"servers": []map[string]interface{}{
					{
						"address":  addr,
						"method":   method,
						"ota":      false,
						"password": password,
						"port":     port,
						"level":    1,
					},
				},
			},
			"streamSettings": map[string]interface{}{
				"network": "tcp",
			},
			"mux": map[string]interface{}{
				"enabled":     false,
				"concurrency": -1,
			},
		},
	}
	result := make(map[string]interface{})
	result[obj["pid"].(string)] = obj
	return result, nil
}

func parseVmessToMap(url string) (map[string]interface{}, error) {
	// 解析 Vmess 协议 URL 的相关信息
	vmessParts, err := tryBase64Decode(url[8:])
	if err != nil {
		return nil, err
	}
	// 将 JSON 字符串解析为 map[string]interface{}
	var obj map[string]interface{}
	err = json.Unmarshal([]byte(vmessParts), &obj)
	if err != nil {
		return nil, err
	}
	obj["pid"] = obj["add"].(string) + ":" + obj["port"].(string)
	obj["type"] = "vmess"
	port, err := strconv.Atoi(obj["port"].(string))
	obj["port"] = port
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
	result := make(map[string]interface{})
	result[obj["pid"].(string)] = obj
	return result, nil
}

var subscribeContentCache = ""

func parseHTTPToMap(url string) (map[string]interface{}, error) {
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
	if subscribeContentCache == content {
		return nil, errors.New("STATE_SUBSCRIPTION_NOT_UPDATE")
	}
	subscribeContentCache = content
	// 解码并获取协议链接
	decodedContent, err := tryBase64Decode(content)
	if err != nil {
		return map[string]interface{}{}, err
	}
	urls := strings.Split(decodedContent, "\n")
	result := make(map[string]interface{})
	for _, urlz := range urls {
		obj, err := parseURLToMap(urlz)
		if err != nil {
			fmt.Println(err.Error() + " : " + urlz)
			continue
		}
		for key, value := range obj {
			result[key] = value
		}
	}
	// 将结果以 JSON 数组的形式返回
	return result, nil
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

func tryBase64Decode(code string) (string, error) {
	decode, err1 := base64.RawStdEncoding.DecodeString(code)
	if err1 != nil {
		decode, err2 := base64.StdEncoding.DecodeString(code)
		if err2 != nil {
			return "", errors.New("vmess base decode error: " + code + "\n" + err1.Error() + "\n" + err2.Error())
		}
		return string(decode), nil
	}
	return string(decode), nil
}
