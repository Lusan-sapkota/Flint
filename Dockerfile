FROM golang:1.26-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /flint .

# Alpine rather than scratch: the approved shell tool runs commands with sh,
# so the image needs a shell and basic coreutils.
FROM alpine:3.22
RUN adduser -D flint && mkdir /data && chown flint /data
COPY --from=build /flint /app/backend/flint
COPY frontend/ /app/frontend/
USER flint
# The binary resolves templates and static files relative to ../frontend.
WORKDIR /app/backend
ENV DB_PATH=/data/chat.db ATTACHMENTS_DIR=/data/attachments
VOLUME /data
EXPOSE 8080
CMD ["./flint"]
