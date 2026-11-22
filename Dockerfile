# Every command under cmd/ goes into the image; /server is the default.
# Module ai links sherpa-onnx and onnxruntime through cgo, so the build has
# cgo on and the runtime has glibc, the two libraries in /usr/local/lib and
# ffmpeg. The models are not in the image: /fetch-models.sh fills /models.
# The access log's country database is in the image, /geoip/country.mmdb.
FROM golang:1.27.3-trixie AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/bin/ ./cmd/... \
 && mkdir -p /out/lib \
 && lib="$(go list -m -f '{{.Dir}}' github.com/k2-fsa/sherpa-onnx-go-linux)/lib/$(uname -m)-unknown-linux-gnu" \
 && cp "$lib/libsherpa-onnx-c-api.so" "$lib/libonnxruntime.so" /out/lib/

FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg \
 && rm -rf /var/lib/apt/lists/*
# Every stack's GEOIP_DB names this file.
RUN --mount=type=bind,source=scripts/fetch-geoip.sh,target=/fetch-geoip.sh sh /fetch-geoip.sh /geoip/country.mmdb
# The binaries' runpath names the build's module cache; ldconfig makes the
# loader find the libraries here instead.
COPY --from=build /out/lib/ /usr/local/lib/
RUN ldconfig \
 && groupadd -g 65532 nonroot && useradd -u 65532 -g 65532 -d /home/nonroot -M -s /usr/sbin/nologin nonroot \
 && mkdir -p /home/nonroot /models && chown 65532:65532 /home/nonroot /models
COPY --from=build /out/bin/ /
COPY --chmod=755 scripts/fetch-models.sh /fetch-models.sh
USER 65532:65532
EXPOSE 8080 9080
ENTRYPOINT ["/server"]
