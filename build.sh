#!/bin/bash

set -e

# Build the 3270Connect binary for Linux
export GOARCH=amd64
export GOOS=linux
go build -o 3270Connect go3270Connect.go

# Prompt for Docker registry credentials
read -p "Enter Docker username: " DOCKER_USERNAME
read -s -p "Enter Docker password: " DOCKER_PASSWORD
echo

# Login to Docker registry
echo $DOCKER_PASSWORD | docker login --username $DOCKER_USERNAME --password-stdin reg.jnnn.gs
if [ $? -ne 0 ]; then
    echo "Docker login failed"
    exit 1
fi

# Build the Docker image
docker build -t 3270connect-linux:latest -f Dockerfile.linux .
if [ $? -ne 0 ]; then
    echo "Docker image build failed"
    exit 1
fi

# Tag the Docker image
docker tag 3270connect-linux:latest reg.jnnn.gs/3270connect/3270connect-linux:latest
if [ $? -ne 0 ]; then
    echo "Docker image tagging failed"
    exit 1
fi

# Push the Docker image
docker push reg.jnnn.gs/3270connect/3270connect-linux:latest
if [ $? -ne 0 ]; then
    echo "Docker image push failed"
    exit 1
fi

echo "Docker image pushed successfully to reg.jnnn.gs/3270connect/3270connect-linux:latest"
