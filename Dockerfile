FROM registry.access.redhat.com/ubi8/ubi-minimal

ARG GO_VERSION=1.12.13
ARG OPERATOR_SDK_VERSION=0.12.0

ARG GOPATH="/go"

RUN mkdir -p /go

# Stage 1 - Install dependencies
RUN microdnf install \
            curl \
            bash \
            git \
            tar \
            make \
            findutils \
            which

RUN curl -O https://storage.googleapis.com/golang/go$GO_VERSION.linux-amd64.tar.gz && tar -xvzf go$GO_VERSION.linux-amd64.tar.gz

# Stage 2 - Install operator-sdk
RUN echo $GOPATH/bin/operator-sdk
RUN curl -L https://github.com/operator-framework/operator-sdk/releases/download/v$OPERATOR_SDK_VERSION/operator-sdk-v$OPERATOR_SDK_VERSION-x86_64-linux-gnu -o $GOPATH/bin/operator-sdk \
    && chmod +x $GOPATH/bin/operator-sdk

RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl \
    && chmod +x ./kubectl \
    && mv ./kubectl /usr/local/bin/kubectl

RUN export GO111MODULE=auto

RUN mkdir -p $GOPATH/src/github.com/redhat-developer/jenkins-operator
WORKDIR $GOPATH/src/github.com/redhat-developer/jenkins-operator

RUN mkdir -p /home/builder

ENV DOCKER_TLS_VERIFY   1
ENV DOCKER_CERT_PATH    /minikube/certs

ENTRYPOINT bash
