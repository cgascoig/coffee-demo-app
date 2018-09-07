FROM golang:1.10 AS build-env
WORKDIR /go/src/app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -o /coffee-demo-app .

FROM alpine:3.6
EXPOSE 5000
COPY --from=build-env /coffee-demo-app /coffee-demo-app
COPY static /static
CMD [ "/coffee-demo-app" ]
