FROM golang:1.16.2-alpine AS builder

WORKDIR /go/src/app
COPY . .

RUN GOOS=linux go build -o image-clone-controller

FROM alpine:3.13.2
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder go/src/app/image-clone-controller .
CMD ["./image-clone-controller"]

