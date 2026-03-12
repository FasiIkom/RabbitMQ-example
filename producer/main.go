package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"rabbitmq-demo/shared"
)

const (
	QueueName    = "task_queue"
	ExchangeName = ""
	RabbitMQURL  = "amqp://guest:guest@rabbitmq:5672/"
	HTTPPort     = ":8080"
)

var taskTypes = []shared.TaskType{
	shared.TaskTypeEasy,
	shared.TaskTypeMedium,
	shared.TaskTypeHard,
	shared.TaskTypeVeryHard,
}

var seq atomic.Int64
var totalSent atomic.Int64

func connectRabbitMQ() (*amqp.Connection, *amqp.Channel, error) {
	var conn *amqp.Connection
	var err error

	for i := 0; i < 10; i++ {
		conn, err = amqp.Dial(RabbitMQURL)
		if err == nil {
			break
		}
		log.Printf("⏳ Waiting for RabbitMQ... attempt %d/10 (%v)", i+1, err)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect after retries: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open channel: %w", err)
	}

	_, err = ch.QueueDeclare(QueueName, true, false, false, false, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	return conn, ch, nil
}

func generateTask(taskType *shared.TaskType) shared.Task {
	t := taskTypes[rand.Intn(len(taskTypes))]
	if taskType != nil {
		t = *taskType
	}
	n := seq.Add(1)
	return shared.Task{
		ID:        fmt.Sprintf("task-%04d", n),
		Type:      t,
		Payload:   fmt.Sprintf("payload-%d", n),
		Priority:  rand.Intn(3) + 1,
		CreatedAt: time.Now(),
	}
}

func publishTask(ch *amqp.Channel, task shared.Task) error {
	body, err := json.Marshal(task)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return ch.PublishWithContext(ctx,
		ExchangeName,
		QueueName,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
			MessageId:    task.ID,
			Timestamp:    task.CreatedAt,
		},
	)
}

var openAPISpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Task Queue Producer API",
    "description": "HTTP API untuk mengirim task ke RabbitMQ queue. Task akan didistribusikan secara merata ke worker-worker yang sedang berjalan (competing consumers pattern).",
    "version": "1.0.0"
  },
  "servers": [
    { "url": "http://localhost:8080", "description": "Local Docker" }
  ],
  "tags": [
    { "name": "Tasks", "description": "Publish task ke queue" },
    { "name": "Monitoring", "description": "Stats dan health check" }
  ],
  "paths": {
    "/task": {
      "post": {
        "tags": ["Tasks"],
        "summary": "Kirim satu task",
        "description": "Publish satu task ke RabbitMQ. Jika parameter 'type' tidak diberikan, type akan dipilih secara random.",
        "parameters": [
          {
            "name": "type",
            "in": "query",
            "required": false,
            "description": "Jenis task — menentukan berapa lama worker memproses. EASY: 0.2-0.6s | MEDIUM: 1-2.5s | HARD: 3-6s | VERY_HARD: 7-12s",
            "schema": {
              "type": "string",
              "enum": ["EASY", "MEDIUM", "HARD", "VERY_HARD"],
              "example": "MEDIUM"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Task berhasil dikirim ke queue",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Task" },
                "examples": {
                  "easy": {
                    "summary": "EASY task",
                    "value": { "id": "task-0001", "type": "EASY", "payload": "payload-1", "priority": 2, "created_at": "2026-03-12T01:30:00Z" }
                  },
                  "very_hard": {
                    "summary": "VERY_HARD task",
                    "value": { "id": "task-0002", "type": "VERY_HARD", "payload": "payload-2", "priority": 3, "created_at": "2026-03-12T01:30:01Z" }
                  }
                }
              }
            }
          },
          "405": { "description": "Method not allowed — gunakan POST" },
          "500": { "description": "Gagal publish ke RabbitMQ" }
        }
      }
    },
    "/task/batch": {
      "post": {
        "tags": ["Tasks"],
        "summary": "Kirim banyak task sekaligus",
        "description": "Publish sejumlah task secara batch ke RabbitMQ. Semua task langsung masuk queue dan didistribusikan ke worker yang tersedia.",
        "parameters": [
          {
            "name": "count",
            "in": "query",
            "required": true,
            "description": "Jumlah task yang akan dikirim (1–100)",
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 100,
              "example": 10
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Batch task berhasil dikirim",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/BatchResponse" },
                "example": {
                  "sent": 3,
                  "tasks": [
                    { "id": "task-0003", "type": "HARD", "payload": "payload-3", "priority": 1, "created_at": "2026-03-12T01:30:02Z" },
                    { "id": "task-0004", "type": "EASY", "payload": "payload-4", "priority": 2, "created_at": "2026-03-12T01:30:02Z" },
                    { "id": "task-0005", "type": "MEDIUM", "payload": "payload-5", "priority": 3, "created_at": "2026-03-12T01:30:02Z" }
                  ]
                }
              }
            }
          },
          "400": { "description": "count tidak valid — harus antara 1 dan 100" },
          "405": { "description": "Method not allowed — gunakan POST" },
          "500": { "description": "Gagal publish ke RabbitMQ" }
        }
      }
    },
    "/stats": {
      "get": {
        "tags": ["Monitoring"],
        "summary": "Producer stats",
        "description": "Statistik producer: total task dikirim, last sequence number, dan task types yang tersedia.",
        "responses": {
          "200": {
            "description": "Stats berhasil diambil",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Stats" },
                "example": {
                  "total_sent": 42,
                  "last_seq": 42,
                  "queue": "task_queue",
                  "task_types": ["EASY", "MEDIUM", "HARD", "VERY_HARD"]
                }
              }
            }
          }
        }
      }
    },
    "/health": {
      "get": {
        "tags": ["Monitoring"],
        "summary": "Health check",
        "description": "Cek apakah producer service sedang berjalan.",
        "responses": {
          "200": {
            "description": "Service sehat",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/Health" },
                "example": { "status": "ok", "queue": "task_queue" }
              }
            }
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "Task": {
        "type": "object",
        "properties": {
          "id":         { "type": "string", "example": "task-0001", "description": "ID unik task (format: task-NNNN)" },
          "type":       { "type": "string", "enum": ["EASY", "MEDIUM", "HARD", "VERY_HARD"], "description": "Jenis task" },
          "payload":    { "type": "string", "example": "payload-1" },
          "priority":   { "type": "integer", "minimum": 1, "maximum": 3, "example": 2, "description": "1=LOW 2=MED 3=HIGH" },
          "created_at": { "type": "string", "format": "date-time" }
        }
      },
      "BatchResponse": {
        "type": "object",
        "properties": {
          "sent":  { "type": "integer", "example": 10 },
          "tasks": { "type": "array", "items": { "$ref": "#/components/schemas/Task" } }
        }
      },
      "Stats": {
        "type": "object",
        "properties": {
          "total_sent": { "type": "integer", "example": 42 },
          "last_seq":   { "type": "integer", "example": 42 },
          "queue":      { "type": "string", "example": "task_queue" },
          "task_types": { "type": "array", "items": { "type": "string" } }
        }
      },
      "Health": {
        "type": "object",
        "properties": {
          "status": { "type": "string", "example": "ok" },
          "queue":  { "type": "string", "example": "task_queue" }
        }
      }
    }
  }
}`

func startHTTPServer(ch *amqp.Channel) {
	mux := http.NewServeMux()

	// POST /task
	mux.HandleFunc("/task", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var taskType *shared.TaskType
		if t := r.URL.Query().Get("type"); t != "" {
			tt := shared.TaskType(t)
			taskType = &tt
		}

		task := generateTask(taskType)
		if err := publishTask(ch, task); err != nil {
			http.Error(w, fmt.Sprintf("failed to publish: %v", err), http.StatusInternalServerError)
			return
		}

		totalSent.Add(1)
		log.Printf("📨 [HTTP] Sent %s | type=%s", task.ID, task.Type)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(task)
	})

	// POST /task/batch
	mux.HandleFunc("/task/batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		countStr := r.URL.Query().Get("count")
		count, err := strconv.Atoi(countStr)
		if err != nil || count < 1 || count > 100 {
			http.Error(w, "count must be between 1 and 100", http.StatusBadRequest)
			return
		}

		tasks := make([]shared.Task, 0, count)
		for i := 0; i < count; i++ {
			task := generateTask(nil)
			if err := publishTask(ch, task); err != nil {
				http.Error(w, fmt.Sprintf("failed to publish task %d: %v", i+1, err), http.StatusInternalServerError)
				return
			}
			tasks = append(tasks, task)
			totalSent.Add(1)
		}

		log.Printf("📨 [HTTP] Batch sent %d tasks", count)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"sent":  count,
			"tasks": tasks,
		})
	})

	// GET /stats
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"total_sent": totalSent.Load(),
			"last_seq":   seq.Load(),
			"queue":      QueueName,
			"task_types": taskTypes,
		})
	})

	// GET /health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"queue":  QueueName,
		})
	})

	// GET /openapi.json
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, openAPISpec)
	})

	// GET /docs — Scalar UI
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html>
<html>
  <head>
    <title>Task Queue API Docs</title>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
  </head>
  <body>
    <script
      id="api-reference"
      data-url="/openapi.json"
      data-configuration='{"theme":"purple"}'></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`)
	})

	log.Printf("🌐 HTTP API listening on %s", HTTPPort)
	log.Printf("   POST /task?type=EASY|MEDIUM|HARD|VERY_HARD")
	log.Printf("   POST /task/batch?count=N")
	log.Printf("   GET  /stats")
	log.Printf("   GET  /health")
	log.Printf("   GET  /docs  ← Scalar API Docs")

	if err := http.ListenAndServe(HTTPPort, mux); err != nil {
		log.Fatalf("❌ HTTP server error: %v", err)
	}
}

func main() {
	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[PRODUCER] ")

	conn, ch, err := connectRabbitMQ()
	if err != nil {
		log.Fatalf("❌ Connection failed: %v", err)
	}
	defer conn.Close()
	defer ch.Close()

	log.Println("✅ Connected to RabbitMQ")

	go startHTTPServer(ch)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Println("🕹️  Manual mode — use HTTP API to publish tasks")
	log.Println("📖 API Docs → http://localhost:8080/docs")

	<-quit
	log.Printf("🛑 Shutting down. Total tasks sent: %d", totalSent.Load())
}