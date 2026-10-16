FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd \
 && CGO_ENABLED=0 go build -o /out/seed ./cmd/seed

FROM alpine:3.22
RUN adduser -D -u 10001 app
COPY --from=build /out/ /usr/local/bin/
USER app
EXPOSE 8080
CMD ["api"]
