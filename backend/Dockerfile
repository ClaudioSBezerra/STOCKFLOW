# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/api .
RUN CGO_ENABLED=0 go build -o /out/seed-admin ./cmd/seed-admin
RUN CGO_ENABLED=0 go build -o /out/seed-dono-plataforma ./cmd/seed-dono-plataforma
RUN CGO_ENABLED=0 go build -o /out/migrar-multi-empresa ./cmd/migrar-multi-empresa
RUN CGO_ENABLED=0 go build -o /out/migrar-saldo-lotes ./cmd/migrar-saldo-lotes
RUN CGO_ENABLED=0 go build -o /out/migrar-estoques-filial ./cmd/migrar-estoques-filial
RUN CGO_ENABLED=0 go build -o /out/seed-fotos-treinamento ./cmd/seed-fotos-treinamento

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
RUN adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=build /out/api ./api
COPY --from=build /out/seed-admin ./seed-admin
COPY --from=build /out/seed-dono-plataforma ./seed-dono-plataforma
COPY --from=build /out/migrar-multi-empresa ./migrar-multi-empresa
COPY --from=build /out/migrar-saldo-lotes ./migrar-saldo-lotes
COPY --from=build /out/migrar-estoques-filial ./migrar-estoques-filial
COPY --from=build /out/seed-fotos-treinamento ./seed-fotos-treinamento

USER appuser

EXPOSE 8080

ENTRYPOINT ["./api"]
