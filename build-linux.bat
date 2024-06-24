@echo off
setlocal

REM Build the 3270Connect binary for Linux
set GOARCH=amd64
set GOOS=linux
go build -o 3270Connect go3270Connect.go

REM Prompt for Docker registry credentials
set /p DOCKER_USERNAME=Enter Docker username: 
set /p DOCKER_PASSWORD=Enter Docker password: 

REM Login to Docker registry
docker login --username %DOCKER_USERNAME% --password %DOCKER_PASSWORD% 
if errorlevel 1 (
    echo Docker login failed
    exit /b 1
)

REM Build the Docker image
docker build -t 3270connect-linux:latest -f Dockerfile.linux .
if errorlevel 1 (
    echo Docker image build failed
    exit /b 1
)

REM Tag the Docker image
docker tag 3270connect-linux:latest 3270io/3270connect-linux:latest
if errorlevel 1 (
    echo Docker image tagging failed
    exit /b 1
)

REM Push the Docker image
docker push 3270io/3270connect-linux:latest
if errorlevel 1 (
    echo Docker image push failed
    exit /b 1
)

echo Docker image pushed successfully to 3270io/3270connect-linux:latest
