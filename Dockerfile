# Build the application from source
FROM golang:1.24.2 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /server main.go

# Deploy the application binary into a lean image
FROM alpine:3.21.3 AS build-release-stage

RUN apk update \
  && apk --no-cache add ca-certificates \
  && apk --no-cache add -U tzdata \
  && rm -rf /var/cache/apk/*

WORKDIR /usr/app/

COPY --from=build-stage server .
COPY --from=build-stage app/config/* config/

EXPOSE 8080
ENTRYPOINT ["./server"]