FROM golang:1.25-alpine AS builder
ARG TARGET=server
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/bin ./cmd/${TARGET}

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/bin /app/bin
CMD ["/app/bin"]
