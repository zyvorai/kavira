# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/kavira ./cmd/kavira

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="KAVIRA" \
      org.opencontainers.image.description="Turn an outage into a reproducible test" \
      org.opencontainers.image.source="https://github.com/zyvorai/kavira" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=build /out/kavira /kavira
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=3s --retries=3 CMD ["/kavira", "healthcheck"]
ENTRYPOINT ["/kavira"]
CMD ["serve", "-addr", "0.0.0.0:8080"]
