FROM golang:1.26-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /flint .

# Alpine rather than scratch: the approved shell tool runs commands with sh,
# so the image needs a shell and basic coreutils.
FROM alpine:3.22
# uid 1000 matches the usual first user on Linux, so a bind-mounted data
# folder that user created is writable from inside the container.
RUN adduser -D -u 1000 flint && mkdir /data && chown flint /data
COPY --from=build /flint /app/backend/flint
COPY frontend/ /app/frontend/
USER flint
# The binary resolves templates and static files relative to ../frontend.
WORKDIR /app/backend
# 0.0.0.0 so a plain `docker run -p` can reach it; docker-compose.yml runs on
# the host network and narrows this back to 127.0.0.1.
ENV DB_PATH=/data/chat.db ATTACHMENTS_DIR=/data/attachments HOST=0.0.0.0
VOLUME /data
EXPOSE 8080
CMD ["./flint"]
