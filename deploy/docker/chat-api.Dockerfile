FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -o /out/chat-api ./services/chat-api/cmd/server

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/chat-api /chat-api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/chat-api"]
