FROM --platform=$BUILDPLATFORM golang:1.23.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /prices .

FROM debian:bookworm-slim
COPY --from=build /prices /usr/local/bin/prices
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/prices"]
