FROM golang:1.24-alpine

RUN apk add --no-cache git ca-certificates tzdata curl wget

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY internal/ internal/
COPY configs/ configs/

RUN mkdir -p data logs

ENV LOG_LEVEL=debug \
    LOG_FORMAT=text \
    ENVIRONMENT=dev

EXPOSE 9002

CMD ["go", "run", "./cmd/llm-proxy"]
