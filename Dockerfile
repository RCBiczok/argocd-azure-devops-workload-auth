FROM golang:1.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY main.go .

RUN go build -o main .

FROM debian:stable-slim

RUN apt-get update
RUN apt-get install -y ca-certificates

WORKDIR /root/

COPY --from=builder /app/main .

ENV ARGOCD_NAMESPACE=""
ENV ARGOCD_SECRET=""
ENV ARGOCD_SA=""

CMD ["./main"]