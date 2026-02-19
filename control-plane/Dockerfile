FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /orchestrator ./cmd/orchestrator

FROM alpine:3.19
RUN apk add --no-cache curl
COPY --from=builder /orchestrator /orchestrator
ENTRYPOINT ["/orchestrator"]