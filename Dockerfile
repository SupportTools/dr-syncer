# Use golang alpine image as the builder stage
FROM golang:1.23.5-alpine AS builder

# Install git for fetching dependencies
RUN apk add --no-cache git

# Set the Current Working Directory inside the container
WORKDIR /src

# Copy go.mod and go.sum first for caching dependencies
COPY go.mod go.sum ./

# Download dependencies with module and build cache
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the rest of the files
COPY . .

# # Run tests with module and build cache
# RUN --mount=type=cache,target=/go/pkg/mod \
#     --mount=type=cache,target=/root/.cache/go-build \
#     CGO_ENABLED=0 go test -v ./...

# Version and Git Commit build arguments
ARG VERSION
ARG GIT_COMMIT
ARG BUILD_DATE

# Build the Go app with versioning information and caching
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X github.com/supporttools/dr-syncer/pkg/version.Version=${VERSION} \
    -X github.com/supporttools/dr-syncer/pkg/version.GitCommit=${GIT_COMMIT} \
    -X github.com/supporttools/dr-syncer/pkg/version.BuildTime=${BUILD_DATE}" \
    -o /bin/dr-syncer

# Use distroless for a more secure and smaller final image
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy our static executable.
COPY --from=builder /bin/dr-syncer /bin/dr-syncer

# Run the binary.
ENTRYPOINT ["/bin/dr-syncer"]
