FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o linuxdo-invitecode .

FROM alpine:3.21
RUN apk --no-cache add ca-certificates && \
    adduser -D -u 1000 appuser
WORKDIR /app
COPY --from=builder /app/linuxdo-invitecode .
COPY --from=builder /app/static ./static
USER appuser
EXPOSE 7386
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:7386/ || exit 1
CMD ["./linuxdo-invitecode"]
