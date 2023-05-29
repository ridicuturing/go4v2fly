FROM v2fly/v2fly-core:v5.4.1

COPY go4v2fly /usr/local/bin/go4v2fly

EXPOSE 1080 1081

ENTRYPOINT ["go4v2fly"]

docker run -d --name go4v2fly-container go4v2fly -url https://justmysocks3.net/members/getsub.php?service=123144&id=9db9ccfe-51ad-401a-8f00-08db4d9c3023