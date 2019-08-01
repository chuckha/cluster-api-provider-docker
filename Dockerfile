# Copyright 2019 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM golang:1.12.7
ARG KUBECTL_VERSION=1.14.3
WORKDIR /cluster-api-provider-docker
RUN curl -L https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-amd64.tar.gz | tar xvz
RUN curl https://get.docker.com | sh
ENV GOPROXY=https://proxy.golang.org
ADD go.mod .
ADD go.sum .
RUN go mod download
ADD cmd cmd
ADD api api
ADD actuators actuators
ADD controllers controllers
ADD kind kind
ADD third_party third_party
COPY third_party/forked/rerun-process-wrapper/start.sh /start.sh
COPY third_party/forked/rerun-process-wrapper/restart.sh /restart.sh
RUN go install -v ./cmd/manager
RUN mv /go/bin/manager /manager
ENTRYPOINT ["/start.sh", "/manager"]
