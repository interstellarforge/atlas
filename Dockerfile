FROM node:24-alpine AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.27-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/atlas ./cmd/atlas \
 && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/atlas /atlas
# The nonroot user must own the volume mount point, or the first start cannot write.
COPY --from=build --chown=65532:65532 /out/data /data
ENV ATLAS_DATA_DIR=/data ATLAS_ADDR=:8080
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/atlas"]
