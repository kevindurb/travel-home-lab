# syntax=docker/dockerfile:1

# Build the Go server on the native builder platform and cross-compile it for
# the target (the Pi is linux/arm64).
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p /out/tmp /out/run/dbus /out/etc/travel-tv && \
    touch /out/tmp/.keep /out/run/dbus/.keep /out/etc/travel-tv/.keep && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" \
    -o /out/travel-tv ./cmd/travel-tv

# wtype and wlrctl are the proven Wayland input helpers. The final image is
# FROM scratch, so copy them and their small musl/Wayland/XKB runtime.
FROM alpine:3.22 AS helpers
RUN apk add --no-cache wtype wlrctl

FROM scratch
COPY --from=build /out/travel-tv /travel-tv
COPY --from=build /out/tmp /tmp
# Mount point for the Ansible-rendered page data.
COPY --from=build /out/etc /etc
# Mount point for the system D-Bus socket (Bluetooth controller pairing).
COPY --from=build /out/run /run
COPY --from=helpers /usr/bin/wtype /usr/bin/wtype
COPY --from=helpers /usr/bin/wlrctl /usr/bin/wlrctl
COPY --from=helpers /lib/ld-musl-*.so.1 /lib/
COPY --from=helpers /usr/lib/libwayland-client.so.0* /usr/lib/
COPY --from=helpers /usr/lib/libxkbcommon.so.0* /usr/lib/
COPY --from=helpers /usr/lib/libffi.so.8* /usr/lib/
ENV PATH=/usr/local/bin:/usr/bin:/bin \
    LANG=C.UTF-8
EXPOSE 8080
ENTRYPOINT ["/travel-tv"]
