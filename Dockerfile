
FROM golang:1.26-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod ./
RUN go mod tidy

COPY . .
RUN CGO_ENABLED=1 go build -o /wa-client .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    qrencode \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /wa-client /app/wa-client

ENV WHATSAPP_DB_PATH=/data/whatsmeow.db

CMD ["/app/wa-client"]
