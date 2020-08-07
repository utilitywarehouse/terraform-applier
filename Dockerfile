FROM golang:1.13-alpine AS build

WORKDIR /go/src/github.com/utilitywarehouse/terraform-applier
COPY . /go/src/github.com/utilitywarehouse/terraform-applier
ENV CGO_ENABLED 0
RUN apk --no-cache add git &&\
  go get -t ./... &&\
  go test ./... &&\
  go build -o /terraform-applier .

FROM alpine:3.10

ENV TERRAFORM_VERSION 0.12.29
COPY templates/ /templates/
COPY static/ /static/
RUN apk --no-cache add ca-certificates git tini &&\
  wget https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip -O terraform.zip &&\
  unzip terraform.zip &&\
  chmod +x terraform &&\
  mv terraform /usr/local/bin/terraform
COPY --from=build /terraform-applier /terraform-applier

ENTRYPOINT ["/sbin/tini", "--"]
CMD [ "/terraform-applier" ]
