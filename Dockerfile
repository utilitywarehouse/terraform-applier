FROM golang:1.16-alpine AS build

WORKDIR /go/src/github.com/utilitywarehouse/terraform-applier
COPY . /go/src/github.com/utilitywarehouse/terraform-applier
ENV CGO_ENABLED 0
RUN apk --no-cache add git \
  && go get -t ./... \
  && go test ./... \
  && go build -o /terraform-applier .

FROM alpine:3.14

COPY templates/ /templates/
COPY static/ /static/
RUN apk --no-cache add ca-certificates git tini
COPY --from=build /terraform-applier /terraform-applier

ENTRYPOINT ["/sbin/tini", "--"]
CMD [ "/terraform-applier" ]
