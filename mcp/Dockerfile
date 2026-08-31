# syntax=docker/dockerfile:1
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mcp-server .

# Distroless nonroot: no shell, static UID 65532.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mcp-server /mcp-server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/mcp-server"]
