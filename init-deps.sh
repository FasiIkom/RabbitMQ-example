#!/bin/bash
set -e

echo "=== Downloading producer dependencies ==="
cd /home/claude/rabbitmq-demo/producer
go mod tidy

echo ""
echo "=== Downloading worker dependencies ==="
cd /home/claude/rabbitmq-demo/worker
go mod tidy

echo ""
echo "✅ All dependencies downloaded"
