FROM golang:1.26-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Release builds pass the tag and commit (docker.yml); a plain build says "dev".
ARG VERSION=dev
ARG COMMIT=
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" -o /flint .

# Alpine, not scratch: the shell tool needs sh.
FROM alpine:3.22
# uid 1000 so a host-created ./data bind mount is writable.
RUN adduser -D -u 1000 flint && mkdir /data && chown flint /data
COPY --from=build /flint /app/backend/flint
COPY frontend/ /app/frontend/
USER flint
# Templates and static files resolve relative to ../frontend.
WORKDIR /app/backend
# 0.0.0.0 for `docker run -p`; docker-compose.yml narrows it to 127.0.0.1.
ENV DB_PATH=/data/chat.db ATTACHMENTS_DIR=/data/attachments HOST=0.0.0.0
VOLUME /data
EXPOSE 8080
CMD ["./flint"]
