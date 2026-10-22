# syntax=docker/dockerfile:1
# Self-contained release image: the portal is embedded in the static binary.
FROM node:22-alpine AS portal
WORKDIR /src/portal
COPY portal/package.json portal/yarn.lock portal/.npmrc ./
RUN --mount=type=secret,id=npmrc,target=/root/.npmrc \
    env -u HTTPS_PROXY -u HTTP_PROXY -u https_proxy -u http_proxy yarn install --frozen-lockfile
COPY portal/ ./
RUN env -u HTTPS_PROXY -u HTTP_PROXY -u https_proxy -u http_proxy yarn build

FROM golang:1.26.8-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=portal /src/portal/dist/ ./internal/infra/portal/dist/
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
