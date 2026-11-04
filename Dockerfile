# Every command under cmd/ goes into the image; /server is the default.
FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/ /
EXPOSE 8080 9080
ENTRYPOINT ["/server"]
