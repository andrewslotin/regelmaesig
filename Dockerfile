FROM golang:1.26 AS builder

WORKDIR /src
COPY go.mod .
COPY *.go .

RUN CGO_ENABLED=0 go build -o regelmaesig .

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /src/regelmaesig /regelmaesig

EXPOSE 8080

ENTRYPOINT ["/regelmaesig"]
