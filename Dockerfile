# Dockerfile for the guesswhoservice service

# ---- Builder Stage ----
FROM golang:1.24.4-alpine AS builder

# Set the working directory for the entire build operation
WORKDIR /app

# Copy the entire repository content (the build context) into the container
COPY . .

# Change to the service's directory and then run the build.
# This allows Go to correctly resolve the `replace` directive in go.mod
# (e.g., `../pkg`) relative to the monorepo structure inside the container.
RUN cd guesswhoservice && go mod tidy && CGO_ENABLED=0 go build -o /app/server ./cmd/server

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