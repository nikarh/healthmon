# Build UI
FROM node:24.13.1-bullseye AS webbuild
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build backend
FROM golang:1.25-bullseye AS gobuild
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=webbuild /app/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/healthmon ./cmd/healthmon

# Final
FROM scratch
COPY --from=gobuild /out/healthmon /healthmon
EXPOSE 8080
ENTRYPOINT ["/healthmon"]
