#!/bin/bash
# ============================================================
# run.sh — Helper script untuk menjalankan demo
# ============================================================

set -e
BOLD='\033[1m'
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[0;33m'
NC='\033[0m'

header() { echo -e "\n${BOLD}${CYAN}▶ $1${NC}"; }
ok()     { echo -e "${GREEN}✅ $1${NC}"; }
info()   { echo -e "${YELLOW}ℹ  $1${NC}"; }

case "$1" in
  up)
    header "Starting all services (RabbitMQ + Producer + 3 Workers)"
    docker compose up --build -d
    ok "All containers started"
    info "RabbitMQ Management UI → http://localhost:15672 (guest/guest)"
    info "View logs: ./run.sh logs"
    ;;

  logs)
    header "Streaming all logs (Ctrl+C to stop)"
    docker compose logs -f --tail=30
    ;;

  logs-workers)
    header "Streaming worker logs only"
    docker compose logs -f --tail=30 worker-alpha worker-beta worker-gamma
    ;;

  stats)
    header "Container status"
    docker compose ps
    echo ""
    header "Queue stats (via RabbitMQ API)"
    curl -s -u guest:guest http://localhost:15672/api/queues/%2F/task_queue \
      | python3 -c "
import sys, json
q = json.load(sys.stdin)
print(f'  Messages ready    : {q.get(\"messages_ready\", 0)}')
print(f'  Messages unacked  : {q.get(\"messages_unacknowledged\", 0)}')
print(f'  Total delivered   : {q.get(\"message_stats\", {}).get(\"deliver_get\", {}).get(\"total\", 0)}')
print(f'  Consumers active  : {q.get(\"consumers\", 0)}')
" 2>/dev/null || info "RabbitMQ API not ready yet, try again in a moment"
    ;;

  scale)
    N=${2:-5}
    header "Scaling workers to $N instances"
    docker compose up -d --scale worker-alpha=1 --scale worker-beta=1 --scale worker-gamma=$N
    ok "Workers scaled"
    ;;

  down)
    header "Stopping all services"
    docker compose down
    ok "All containers stopped"
    ;;

  clean)
    header "Removing all containers, images, and volumes"
    docker compose down --rmi all --volumes
    ok "Cleaned up"
    ;;

  *)
    echo -e "${BOLD}Usage:${NC}"
    echo "  ./run.sh up            — Build & start everything"
    echo "  ./run.sh logs          — Stream all logs"
    echo "  ./run.sh logs-workers  — Stream worker logs only"
    echo "  ./run.sh stats         — Show queue stats"
    echo "  ./run.sh down          — Stop everything"
    echo "  ./run.sh clean         — Remove all containers & images"
    ;;
esac
