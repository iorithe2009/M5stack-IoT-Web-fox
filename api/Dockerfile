# ===== build stage =====
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
# go.sumは初回は無くてもOK。ビルド時に自動生成されます。
RUN go mod download || true
COPY . ./
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

# ===== run stage =====
FROM alpine:3.20
WORKDIR /app
RUN adduser -D -H appuser
COPY --from=build /out/server /app/server
USER appuser
EXPOSE 8080
CMD ["/app/server"]
