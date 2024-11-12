FROM golang:1.23.1-alpine AS builder

WORKDIR /build
COPY ./chat .
RUN go mod download
RUN CGO_ENABLED=0 go build -o ./tagged_chat

FROM alpine AS final

WORKDIR /chat
COPY --from=builder /build/tagged_chat tagged_chat
COPY ./chat/static ./static
COPY ./chat/index.html index.html
EXPOSE 8001
CMD sleep 30 && ./tagged_chat