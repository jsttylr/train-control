# Small image: just the one program, no operating system inside.
FROM --platform=$BUILDPLATFORM golang:1.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/train-control .

FROM scratch
COPY --from=build /out/train-control /train-control
# The roster lives in /data; keep it on a volume so it survives updates.
ENV TRAIN_CONTROL_DATA=/data TRAIN_CONTROL_LISTEN=0.0.0.0:8070 TRAIN_CONTROL_NO_BROWSER=1
VOLUME /data
# Unraid reads these to show the icon and an "open the page" link however the container was added.
LABEL net.unraid.docker.icon="https://raw.githubusercontent.com/jsttylr/train-control/main/unraid/icon.png" \
      net.unraid.docker.webui="http://[IP]:[PORT:8070]/" \
      org.opencontainers.image.source="https://github.com/jsttylr/train-control"
EXPOSE 8070/tcp
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/train-control", "-healthcheck"]
ENTRYPOINT ["/train-control"]
