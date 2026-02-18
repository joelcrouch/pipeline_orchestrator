FROM alpine:3.19

# iproute2 = ip + tc commands
# iputils  = ping (for healthcheck)
RUN apk add --no-cache iproute2 iputils bash

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]