FROM golang:1.20 as builder
WORKDIR /work
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# RUN go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o myao .


FROM gcr.io/distroless/static@sha256:3c5767883bc3ad6c4ad7caf97f313e482f500f2c214f78e452ac1ca932e1bf7f
COPY --from=builder /work/myao /bin/myao
ENTRYPOINT ["/bin/myao"]
