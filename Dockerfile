FROM v2fly/v2fly-core:v5.4.1

COPY go4v2fly /usr/local/bin/go4v2fly

EXPOSE 1080 1081

ENTRYPOINT ["go4v2fly"]

