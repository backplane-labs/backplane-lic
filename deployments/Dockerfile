# Self-contained release image: static Go binary on distroless.
FROM golang:1.26.8-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=1970-01-01T00:00:00Z
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-X github.com/backplane-labs/backplane/pkg/backplane/build.Service=lic -X github.com/backplane-labs/backplane/pkg/backplane/build.Version=${VERSION} -X github.com/backplane-labs/backplane/pkg/backplane/build.Commit=${COMMIT} -X github.com/backplane-labs/backplane/pkg/backplane/build.Date=${DATE}" \
    -o /out/lic ./cmd/lic

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/lic /app/bin/lic
COPY LICENSE /app/LICENSE
EXPOSE 8080 9400
USER nonroot:nonroot
ENTRYPOINT ["/app/bin/lic"]
