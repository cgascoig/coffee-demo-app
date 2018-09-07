FROM golang:1.10 AS build-env
WORKDIR /go/src/app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -o /coffee-demo-app .

FROM alpine:3.6
EXPOSE 5000
COPY static /static
COPY keys /keys
COPY --from=build-env /coffee-demo-app /coffee-demo-app
RUN apk update && apk add --no-cache ca-certificates
CMD [ "/coffee-demo-app" ]
