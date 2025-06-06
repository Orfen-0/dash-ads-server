# Use official Golang image as the build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app

# Copy go.mod and go.sum first for dependency caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Move into the cmd directory where main.go is likely located
WORKDIR /app/cmd

# Build the Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server

# Use a minimal image for the final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
RUN mkdir -p /root/logs
RUN mkdir -p /root/internal/files


# Copy the built server binary from the builder stage
COPY --from=builder /app/server .
COPY internal/files/app.apk /root/internal/files/app.apk

# Expose the ports
EXPOSE 8080
EXPOSE 1935

# Run the server
CMD ["./server"]
