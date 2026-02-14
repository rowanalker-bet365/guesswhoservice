# Dockerfile for the guesswhoservice service

# ---- Build Stage ----
# Use the official Go image as a build environment.
FROM golang:1.24.4-alpine as builder

# Set the working directory inside the container.
WORKDIR /app

# Copy the Go module files and download dependencies.
# This is done in a separate step to leverage Docker layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application source code.
COPY . .

# Build the Go application.
# CGO_ENABLED=0 is used to build a statically linked binary.
# -o /app/server builds the output binary to /app/server.
RUN CGO_ENABLED=0 go build -o /app/server guesswhoserviceapi.go

# Set execute permission on the binary.
RUN chmod +x /app/server

# ---- Production Stage ----
# Use a minimal base image for the final container.
# "scratch" is a completely empty image.
FROM scratch

# Set the working directory.
WORKDIR /app

# Copy the built binary from the builder stage.
COPY --from=builder /app/server .

# Expose the port the application will run on.
# The default for Cloud Run is 8080.
EXPOSE 8080

# Set the command to run when the container starts.
CMD ["/app/server"]