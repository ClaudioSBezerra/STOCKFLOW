# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/api .
RUN CGO_ENABLED=0 go build -o /out/seed-admin ./cmd/seed-admin
RUN CGO_ENABLED=0 go build -o /out/seed-dono-plataforma ./cmd/seed-dono-plataforma

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
RUN adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=build /out/api ./api
COPY --from=build /out/seed-admin ./seed-admin
COPY --from=build /out/seed-dono-plataforma ./seed-dono-plataforma

USER appuser

EXPOSE 8080

ENTRYPOINT ["./api"]
