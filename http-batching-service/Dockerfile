FROM golang:alpine
COPY . /opt/http-batching-service

WORKDIR /opt/http-batching-service
RUN go build

ENTRYPOINT ["./http-batching-service"]
