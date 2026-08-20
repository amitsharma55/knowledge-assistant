FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 go build -o /out/indexer ./services/ingestion/cmd/indexer

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/indexer /indexer
USER nonroot:nonroot
ENTRYPOINT ["/indexer"]
