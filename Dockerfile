FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY db ./db
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /cardplay ./cmd/cardplay
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /cardplay /cardplay
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/cardplay"]
CMD ["serve"]
