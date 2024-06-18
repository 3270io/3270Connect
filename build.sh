#!/bin/bash

# Build the Linux image
docker build --target final-linux -t 3270connect-linux .

# Build the Windows image
#docker build --target final-windows -t 3270connect-windows .
