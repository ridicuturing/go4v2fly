FROM v2fly/v2fly-core:v5.4.1

COPY ./go4v2fly /usr/local/bin/


EXPOSE 1080 1081

ENTRYPOINT []
CMD go4v2fly -url $CONFIG_URL

