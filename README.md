# 🐇 RabbitMQ Event-Driven Demo — Go

Simulasi sistem **event-driven** dengan pola **Competing Consumers** menggunakan RabbitMQ dan Go.

---

## 📐 Arsitektur

```
┌─────────────────────┐     publish      ┌──────────────────┐     consume     ┌──────────────────┐
│                     │ ────────────────▶│                  │────────────────▶│  Worker ALPHA    │
│  Producer           │                  │  task_queue      │                 ├──────────────────┤
│  (HTTP API :8080)   │  via REST / curl │  (RabbitMQ)      │────────────────▶│  Worker BETA     │
│                     │                  │                  │                 ├──────────────────┤
└─────────────────────┘                  └──────────────────┘────────────────▶│  Worker GAMMA    │
         │                                       ▲                             └──────────────────┘
         │                                       │
   http://localhost:8080/docs          http://localhost:15672
   (Scalar API Docs)                   (RabbitMQ Management UI)
```

**Key concept**: Setiap message hanya diproses oleh **satu** worker.
RabbitMQ mendistribusikan task secara merata berdasarkan ketersediaan worker (QoS prefetch=1).

---

## 🗂 Struktur Project

```
rabbitmq-demo/
├── docker-compose.yml       # Orkestrasi semua service
├── go.mod                   # Single module — shared oleh producer & worker
├── go.sum
│
├── shared/
│   └── task.go              # Model Task & TaskResult (shared)
│
├── producer/
│   ├── main.go              # HTTP API + publish task ke queue
│   └── Dockerfile
│
└── worker/
    ├── main.go              # Competing consumer — proses task
    └── Dockerfile
```

---

## 🚀 Cara Menjalankan

### Prerequisites
- Docker Desktop terinstall dan running
- Port `5672`, `15672`, dan `8080` tidak dipakai

### 1. Build & Start

```bash
docker compose up -d --build
```

### 2. Lihat Logs Real-time

```bash
# Semua service
docker compose logs -f

# Worker saja
docker compose logs -f worker-alpha worker-beta worker-gamma

# Producer saja
docker compose logs -f producer
```

### 3. Kirim Task via HTTP API

```bash
# Kirim 1 task random
curl -X POST http://localhost:8080/task

# Kirim task dengan type spesifik
curl -X POST "http://localhost:8080/task?type=EASY"
curl -X POST "http://localhost:8080/task?type=MEDIUM"
curl -X POST "http://localhost:8080/task?type=HARD"
curl -X POST "http://localhost:8080/task?type=VERY_HARD"

# Kirim banyak task sekaligus
curl -X POST "http://localhost:8080/task/batch?count=20"

# Cek stats producer
curl http://localhost:8080/stats
```
Atau alternatifnya bisa menggunakan GUI Scalar (dijelaskan pada no. 4)

### 4. Buka API Docs (Scalar)

Buka browser: **http://localhost:8080/docs**

Dari sini bisa langsung **Try it out** semua endpoint tanpa perlu curl. Cukup pilih `Kirim satu task` untuk membuat 1 task dengan tingkat kesulitan yang bisa dipilih (disertakan di query param) atau `Kirim banyak task sekaligus` untuk membuat >= 1 task dengan tingkat kesulitan random.

### 5. Buka RabbitMQ Management UI

Buka browser: **http://localhost:15672**
- Username: `guest`
- Password: `guest`

Navigasi ke **Queues → task_queue** untuk melihat:
- Message rate (publish vs consume)
- Jumlah consumer aktif
- Ready vs Unacked messages

### 6. Stop

```bash
docker compose down
```

---

## 📋 Task Types

| Type | Durasi Proses | Keterangan |
|---|---|---|
| `EASY` | 0.2s – 0.6s | Task ringan |
| `MEDIUM` | 3s – 27s | Task sedang |
| `HARD` | 10s – 20s | Task berat |
| `VERY_HARD` | 30s – 60s | Task sangat berat |

Semakin berat task, semakin lama worker memprosesnya — berguna untuk mengamati efek load distribution. Diimplementasikan dengan sleep untuk menghemat resource. Asumsi sleep adalah waktu yang diperlukan untuk menjalankan actual tasks.

---

## 🔍 Cara Membaca Logs

### Producer log:
```
[PRODUCER] 10:23:01 📨 [HTTP] Sent task-0001 | type=HARD
[PRODUCER] 10:23:02 📨 [HTTP] Batch sent 10 tasks
```

### Worker log:
```
[WORKER-ALPHA] 10:23:01 ⚙️  Processing task-0001 | type=HARD         | est=~4200ms
[WORKER-BETA]  10:23:01 ⚙️  Processing task-0002 | type=EASY         | est=~350ms
[WORKER-BETA]  10:23:02 ✅ Done     task-0002 | type=EASY         | duration=350ms | total=1 | avg=350ms
[WORKER-ALPHA] 10:23:05 ✅ Done     task-0001 | type=HARD         | duration=4213ms | total=1 | avg=4213ms
```

Perhatikan: task-0001 hanya diproses oleh ALPHA, tidak pernah oleh BETA atau GAMMA.
BETA bisa selesai lebih cepat dan langsung ambil task berikutnya karena dapat EASY task.

---

## 🧠 Mekanisme Kunci

### 1. Durable Queue + Persistent Messages
```go
// Queue tidak hilang jika RabbitMQ restart
ch.QueueDeclare(QueueName, true /*durable*/, ...)

// Message tidak hilang jika RabbitMQ restart
amqp.Publishing{
    DeliveryMode: amqp.Persistent,
}
```

### 2. Manual ACK (Acknowledgement)
```go
// Auto-ack = false → worker harus konfirmasi setelah selesai
ch.Consume(QueueName, ..., false /*auto-ack*/, ...)

// Setelah sukses proses:
msg.Ack(false)

// Jika gagal, requeue agar diambil worker lain:
msg.Nack(false, true /*requeue*/)
```
Tanpa ACK, jika worker mati saat memproses, message akan dikembalikan ke queue otomatis.

### 3. QoS Prefetch = 1 (Fair Dispatch)
```go
ch.Qos(1, 0, false)
```
Setiap worker hanya boleh menerima 1 unacked message sekaligus.
Ini mencegah worker cepat "memborong" semua task — worker yang sedang sibuk tidak akan dapat task baru sampai task sekarang selesai.

---

## ⚖️ Asynchronous vs Request–Response

| Aspek | Request–Response (HTTP biasa) | Asynchronous (RabbitMQ) |
|---|---|---|
| **Coupling** | Ketat — client tahu alamat server | Longgar — client hanya tahu nama queue |
| **Blocking** | Client menunggu response | Client langsung lanjut setelah publish |
| **Availability** | Jika server down, request gagal | Message tetap tersimpan di queue |
| **Scaling** | Scale server harus koordinasi | Tambah worker = otomatis dapat task |
| **Error handling** | Client harus retry sendiri | Broker requeue otomatis jika worker gagal |
| **Throughput** | Terbatas oleh kecepatan server | Producer bisa jauh lebih cepat dari consumer |
| **Use case** | Butuh response segera (query data) | Proses background (email, resize, report) |

---

## 💡 Eksperimen yang Bisa Dicoba

### A. Lihat queue menumpuk lalu terkuras
```bash
# Stop semua worker
docker compose stop worker-alpha worker-beta worker-gamma

# Kirim 30 task
curl -X POST "http://localhost:8080/task/batch?count=30"

# Lihat di http://localhost:15672 → queue menumpuk (Ready: 30)
# Start balik worker, lihat queue langsung terkuras
docker compose start worker-alpha worker-beta worker-gamma
```

### B. Buktikan asynchronous — response instan meski task lama
```bash
# VERY_HARD task butuh 7-12 detik untuk diproses
# Tapi HTTP response balik dalam <1ms
curl -X POST "http://localhost:8080/task?type=VERY_HARD"
```

### C. Matikan satu worker saat ada task
```bash
docker compose stop worker-beta
# Task yang tadinya giliran BETA akan diambil ALPHA atau GAMMA
```

### D. Test durability — restart RabbitMQ
```bash
docker compose restart rabbitmq
# Message yang belum diproses tidak hilang!
```

### E. Lihat efek QoS — tanpa fair dispatch
Edit `worker/main.go`, ubah `ch.Qos(1, 0, false)` → `ch.Qos(0, 0, false)`,
rebuild, dan lihat semua task diborong worker pertama yang connect.