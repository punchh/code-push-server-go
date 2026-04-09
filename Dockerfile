FROM alpine:3.21.3
RUN apk update \
    && apk --no-cache add ca-certificates \
    && apk --no-cache add -U tzdata \
    && rm -rf /var/cache/apk/*

WORKDIR /server
COPY server .

EXPOSE 8080
CMD ["./server"]
