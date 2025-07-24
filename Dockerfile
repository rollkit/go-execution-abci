FROM golang:1.24-alpine AS ignite-builder

# Install dependencies needed for ignite and building
RUN apk add --no-cache \
    libc6-compat \
    curl \
    bash

# Set environment variables
ENV ROLLKIT_VERSION=v1.0.0-beta.2
ENV IGNITE_VERSION=v29.2.0

RUN curl -sSL https://get.ignite.com/cli@${IGNITE_VERSION}! | bash

WORKDIR /workspace

COPY . ./go-execution-abci

RUN ignite scaffold chain gm --no-module --skip-git --address-prefix gm

WORKDIR /workspace/gm

RUN ignite app install github.com/ignite/apps/rollkit@main && \
    ignite rollkit add

RUN go mod edit -replace github.com/rollkit/rollkit=github.com/rollkit/rollkit@${ROLLKIT_VERSION} && \
    go mod edit -replace github.com/rollkit/go-execution-abci=/workspace/go-execution-abci && \
    go mod tidy

RUN ignite chain build --skip-proto

# create lightweight runtime image
FROM alpine:latest

RUN apk add --no-cache ca-certificates

# create non-root user
RUN addgroup -g 10001 -S gm && \
    adduser -u 10001 -S gm -G gm

WORKDIR /home/gm

# copy the built binary from the builder stage
COPY --from=ignite-builder /go/bin/gmd /usr/local/bin/gmd

RUN chown -R gm:gm /home/gm
USER gm

# expose common ports
EXPOSE 26657 26656 9090 1317

CMD ["gmd"]
