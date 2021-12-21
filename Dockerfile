FROM golang as go
WORKDIR /keba
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o keba -ldflags="-s -w -extldflags=-static" -trimpath .

FROM alpine
COPY --from=go /keba/keba /usr/bin/keba
ENTRYPOINT ["/usr/bin/keba"]
