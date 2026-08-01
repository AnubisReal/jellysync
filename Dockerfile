# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/jellysync ./cmd/jellysync

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/jellysync /jellysync
ENV JELLYSYNC_ADDR=:8090 JELLYSYNC_DATA_DIR=/storage/.jellysync JELLYSYNC_STORAGE_ROOT=/storage
VOLUME ["/storage"]
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/jellysync"]
