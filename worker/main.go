package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"rabbitmq-demo/shared"
)

const (
	QueueName   = "task_queue"
	RabbitMQURL = "amqp://guest:guest@rabbitmq:5672/"
)

// Processing duration per task type — semakin hard, semakin lama
var processingTime = map[shared.TaskType]struct{ min, max int }{
	shared.TaskTypeEasy:     {min: 200, max: 600},
	shared.TaskTypeMedium:   {min: 3000, max: 7000},
	shared.TaskTypeHard:     {min: 10000, max: 20000},
	shared.TaskTypeVeryHard: {min: 30000, max: 60000},
}

type Worker struct {
	id      string
	channel *amqp.Channel
	logger  *log.Logger
	stats   struct {
		processed int
		failed    int
		totalMs   int64
	}
}

func NewWorker(id string, ch *amqp.Channel) *Worker {
	return &Worker{
		id:      id,
		channel: ch,
		logger:  log.New(os.Stdout, fmt.Sprintf("[WORKER-%s] ", id), log.Ltime|log.Lmsgprefix),
	}
}

func (w *Worker) processTask(task shared.Task) error {
	pt := processingTime[task.Type]
	duration := time.Duration(pt.min+rand.Intn(pt.max-pt.min)) * time.Millisecond

	w.logger.Printf("⚙️  Processing %s | type=%-12s | est=~%dms",
		task.ID, task.Type, duration.Milliseconds())

	// Simulate work with sleep
	time.Sleep(duration)

	// Simulate 5% random failure
	if rand.Float32() < 0.05 {
		return fmt.Errorf("simulated processing failure")
	}

	w.stats.processed++
	w.stats.totalMs += duration.Milliseconds()
	avgMs := w.stats.totalMs / int64(w.stats.processed)

	w.logger.Printf("✅ Done     %s | type=%-12s | duration=%dms | total=%d | avg=%dms",
		task.ID, task.Type, duration.Milliseconds(), w.stats.processed, avgMs)

	return nil
}

func (w *Worker) Start() error {
	// Fair dispatch — max 1 unacked message per worker at a time
	if err := w.channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	msgs, err := w.channel.Consume(
		QueueName,
		w.id,  // consumer tag
		false, // auto-ack = false (manual ack)
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	w.logger.Printf("👂 Listening on queue: %q", QueueName)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				w.logger.Println("📭 Channel closed")
				return nil
			}

			var task shared.Task
			if err := json.Unmarshal(msg.Body, &task); err != nil {
				w.logger.Printf("❌ Failed to parse message: %v", err)
				msg.Nack(false, false) // discard malformed message
				continue
			}

			if err := w.processTask(task); err != nil {
				w.stats.failed++
				w.logger.Printf("❌ Failed  %s: %v | requeuing...", task.ID, err)
				msg.Nack(false, true) // requeue on failure
			} else {
				msg.Ack(false) // acknowledge success
			}

		case <-quit:
			w.logger.Printf("🛑 Shutting down. processed=%d failed=%d",
				w.stats.processed, w.stats.failed)
			return nil
		}
	}
}

func connectRabbitMQ(workerID string) (*amqp.Connection, *amqp.Channel, error) {
	logger := log.New(os.Stdout, fmt.Sprintf("[WORKER-%s] ", workerID), log.Ltime)
	var conn *amqp.Connection
	var err error

	for i := 0; i < 10; i++ {
		conn, err = amqp.Dial(RabbitMQURL)
		if err == nil {
			break
		}
		logger.Printf("⏳ Waiting for RabbitMQ... attempt %d/10", i+1)
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect: %w", err)
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

func main() {
	workerID := os.Getenv("WORKER_ID")
	if workerID == "" {
		workerID = fmt.Sprintf("W%d", rand.Intn(999)+1)
	}

	conn, ch, err := connectRabbitMQ(workerID)
	if err != nil {
		log.Fatalf("[WORKER-%s] ❌ %v", workerID, err)
	}
	defer conn.Close()
	defer ch.Close()

	worker := NewWorker(workerID, ch)
	if err := worker.Start(); err != nil {
		log.Fatalf("[WORKER-%s] ❌ Worker error: %v", workerID, err)
	}
}