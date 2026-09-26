FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG SERVICE
RUN CGO_ENABLED=0 GOOS=linux go build -o /service ./${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /service /service
USER nonroot:nonroot
ENTRYPOINT ["/service"]
