FROM golang:1.25 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/gomi ./cmd/gomi

FROM gcr.io/distroless/static-debian12
COPY --from=builder /out/gomi /usr/local/bin/gomi
ENTRYPOINT ["/usr/local/bin/gomi"]
