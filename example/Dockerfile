FROM golang:1.26.1

WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build go-trace CLI
RUN go build -o /usr/local/bin/go-trace .

# go-trace instruments, builds, and runs the example app
CMD ["go-trace", "./example"]
