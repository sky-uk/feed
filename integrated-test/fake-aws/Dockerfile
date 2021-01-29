FROM golang:1.15-alpine AS app-builder
WORKDIR /
COPY . .

RUN CGO_ENABLED=0 go build -o /fake-aws -v fake_aws.go

FROM alpine:3.12
COPY --from=app-builder /fake-aws /
ENTRYPOINT ["/fake-aws"]
