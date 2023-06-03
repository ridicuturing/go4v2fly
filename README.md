# go4v2fly

从订阅url中获取代理(仅支持ss和vmess)，并用延迟最低的服务借助v2fly(v2ray)挂上代理。
容器的1080端口做http代理，1081端口做socks代理。

docker地址: https://hub.docker.com/r/hoi4tech/go4v2fly

运行命令: 
```
 docker run -d -p 18888:1080 --name go4v2fly  hoi4tech/go4v2fly -url 'https://justmysocks3.net/members/getsub.php?service=...'
```
url是必填的参数，可以是订阅的http链接，也可以是ss协议和vmess协议的链接。
