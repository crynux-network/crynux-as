FROM golang:1.24-alpine3.23 AS builder
RUN apk add --no-cache --update gcc g++

WORKDIR /crynux_as

COPY go.* .

RUN CGO_ENABLED=1 go mod download

COPY . .

RUN CGO_ENABLED=1 CGO_CFLAGS="-D_LARGEFILE64_SOURCE" go build

FROM alpine:3.23

RUN apk add --no-cache tzdata
ENV TZ=Asia/Tokyo

WORKDIR /app

COPY --from=builder /crynux_as/crynux_as .

CMD ["/app/crynux_as"]
