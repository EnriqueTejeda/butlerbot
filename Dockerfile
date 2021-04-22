FROM golang as builder
ENV GO111MODULE=on
WORKDIR /app/

ADD go.mod go.sum /app/
RUN go mod download

ADD . .
RUN go build -o /app/butlerci main.go

FROM gcr.io/distroless/base
EXPOSE 8080
WORKDIR /
COPY --from=builder /app/butlerci /app/butlerci
ENTRYPOINT ["/app/butlerci","--config","/app/config.yaml"]
