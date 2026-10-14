# Build UI
FROM --platform=$BUILDPLATFORM node:26-trixie AS webbuild
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN mkdir -p /app/cmd/healthmon/web/dist
RUN npm run build

# Build backend
FROM --platform=$BUILDPLATFORM golang:1.27.2-trixie AS gobuild
ARG TARGETOS TARGETARCH
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=webbuild /app/cmd/healthmon/web/dist ./cmd/healthmon/web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/healthmon ./cmd/healthmon

# Final
FROM scratch
COPY --from=gobuild /out/healthmon /healthmon
EXPOSE 8080
ENTRYPOINT ["/healthmon"]
