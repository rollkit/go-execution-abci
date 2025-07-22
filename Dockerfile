FROM golang:1.24-alpine AS ignite-builder

# Install dependencies needed for ignite and building
RUN apk add --no-cache \
    git \
    make \
    curl \
    bash \
    jq

# Set environment variables
ENV DO_NOT_TRACK=true
ENV ROLLKIT_VERSION=v1.0.0-beta.1
ENV IGNITE_VERSION=v29.2.0

# Install Ignite CLI
RUN curl -sSL https://get.ignite.com/cli@${IGNITE_VERSION}! | bash

# Create working directory
WORKDIR /workspace

# Copy the current go-execution-abci source
COPY . ./go-execution-abci

# Scaffold the rollkit chain
RUN ignite scaffold chain gm --no-module --skip-git --address-prefix gm

# Navigate to chain directory and configure it
WORKDIR /workspace/gm

# Install rollkit app and add rollkit
RUN ignite app install github.com/ignite/apps/rollkit@main && \
    ignite rollkit add

# Replace modules with local version and tagged rollkit
RUN go mod edit -replace github.com/rollkit/rollkit=github.com/rollkit/rollkit@${ROLLKIT_VERSION} && \
    go mod edit -replace github.com/rollkit/go-execution-abci=/workspace/go-execution-abci && \
    go mod tidy

# Build the chain
RUN ignite chain build --skip-proto

# Initialize the chain and dump genesis file contents
RUN ignite rollkit init && \
    echo "=== GENESIS FILE CONTENTS ===" && \
    cat ~/.gm/config/genesis.json && \
    echo "=== END GENESIS FILE ===" && \
    ls -la ~/.gm/config/

# Final stage - create lightweight runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN addgroup -g 10001 -S gm && \
    adduser -u 10001 -S gm -G gm

# Set working directory
WORKDIR /home/gm

# Copy the built binary from the builder stage
COPY --from=ignite-builder /go/bin/gmd /usr/local/bin/gmd

# Change ownership and switch to non-root user
RUN chown -R gm:gm /home/gm
USER gm

# Expose common ports
EXPOSE 26657 26656 9090 1317

# Set default command
CMD ["gmd"]