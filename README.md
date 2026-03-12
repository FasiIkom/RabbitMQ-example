# RabbitMQ Event-Driven Demo — Go

Simulasi sistem **event-driven** dengan pola **Competing Consumers** menggunakan RabbitMQ dan Go.

---

## Arsitektur

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

## Struktur Project

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

## Cara Menjalankan

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

## Task Types

| Type | Durasi Proses | Keterangan |
|---|---|---|
| `EASY` | 0.2s – 0.6s | Task ringan |
| `MEDIUM` | 3s – 27s | Task sedang |
| `HARD` | 10s – 20s | Task berat |
| `VERY_HARD` | 30s – 60s | Task sangat berat |

Semakin berat task, semakin lama worker memprosesnya — berguna untuk mengamati efek load distribution. Diimplementasikan dengan sleep untuk menghemat resource. Asumsi sleep adalah waktu yang diperlukan untuk menjalankan actual tasks.

---

## Cara Membaca Logs

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

## Mekanisme Kunci

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

## Asynchronous vs Request–Response

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

## Demonstrasi & Pengujian

### 1. Bagaimana Event Dikirim

Event dikirim melalui HTTP API producer. Ketika `POST /task` dipanggil, producer membuat struct `Task`, men-serialize ke JSON, lalu mempublish ke `task_queue` di RabbitMQ menggunakan protokol AMQP. Producer **tidak menunggu** task selesai diproses — response HTTP langsung dikembalikan begitu message berhasil masuk queue.

```bash
curl -X POST "http://localhost:8080/task?type=HARD"
```

> ![Kirim task via curl atau Scalar](https://github.com/user-attachments/assets/89eaeb90-0a61-4b04-ad23-2af5783486d0)

---

### 2. Bagaimana Event Diproses Consumer Secara Real-time

Begitu worker aktif, RabbitMQ langsung mendistribusikan message ke worker yang available. Setiap worker hanya memproses satu task dalam satu waktu (QoS prefetch=1). Setelah selesai, worker mengirim ACK dan langsung mengambil task berikutnya dari queue.

```bash
docker compose logs -f worker-alpha worker-beta worker-gamma
```

> ![Worker memproses task secara real-time](https://github.com/user-attachments/assets/626e2b60-e99f-422d-b520-e837e1842993)

---

### 3. Bagaimana Event Disimpan di Queue

Message disimpan secara persisten di `task_queue`. Dengan `DeliveryMode: Persistent` dan queue yang dideklarasikan sebagai `durable`, message tidak akan hilang meskipun RabbitMQ di-restart. Hal ini dapat diamati di RabbitMQ Management UI ketika worker di-stop — message akan menumpuk di queue dalam status **Ready**.

```bash
# Stop semua worker, kirim beberapa task, lalu lihat di Management UI
docker compose stop worker-alpha worker-beta worker-gamma
curl -X POST "http://localhost:8080/task/batch?count=10"
```
> ![Mematikan worker](https://github.com/user-attachments/assets/4be82cac-e613-4b8d-b2ff-6acc1237bc97)
> ![Queue menumpuk saat worker di-stop](https://github.com/user-attachments/assets/6a795445-ce7f-4062-9089-93db05fdc4b9)

---

### 4. Pengujian — Kirim Beberapa Event Sekaligus

Mengirim 20 task sekaligus menggunakan batch endpoint untuk mengamati distribusi beban ke ketiga worker.

```bash
curl -X POST "http://localhost:8080/task/batch?count=20"
```

> log distribusi task ke ALPHA, BETA, dan GAMMA_
>
> ![Distribusi task ke semua worker](https://github.com/user-attachments/assets/805fee0d-0955-405b-83ab-b0a18834848d)

> RabbitMQ Management UI menampilkan message rate dan consumer count_
>
> ![RabbitMQ dashboard saat batch task dikirim](https://github.com/user-attachments/assets/805fee0d-0955-405b-83ab-b0a18834848d)

---

### 5. Pengujian — Buktikan Komunikasi Asynchronous

Bukti paling konkret bahwa sistem bersifat asynchronous: mengirim `VERY_HARD` task yang butuh 30–60 detik untuk diproses, namun HTTP response dikembalikan dalam hitungan milidetik.

```bash
curl -X POST "http://localhost:8080/task?type=VERY_HARD"
# Response balik <1ms, padahal task baru selesai diproses 30-60 detik kemudian
```

> response time HTTP vs waktu proses worker di log_
>
> ![Response instan vs proses lama di worker](https://github.com/user-attachments/assets/a9197383-42a9-47df-973d-a756432009ac)
> ![Response instan vs proses lama di worker](https://github.com/user-attachments/assets/f7002ba1-15ee-4af4-9221-a028f17fe794)

Dengan ini, kita mendapatkan keuntungan yaitu server utama tidak terkena blocking dari request besar tersebut sehingga tetap dapat memproses request-request lainnya.

## Asumsi & Keputusan Implementasi

- **Tidak ada database** — task dan hasil proses tidak dipersist ke DB.
  RabbitMQ sudah menyimpan message secara durable (`Persistent + durable queue`),
  dan tujuan demo ini adalah menunjukkan mekanisme event-driven, bukan data persistence.

- **Simulasi proses dengan sleep** — worker menggunakan `time.Sleep()` untuk
  mensimulasikan durasi kerja. Asumsi: sleep merepresentasikan waktu yang dibutuhkan
  actual task (image processing, file backup, dsb).

- **Producer manual (tidak auto-publish)** — task hanya dikirim via HTTP API,
  bukan otomatis. Ini memudahkan demonstrasi dan pengujian secara terkontrol.

## Penggunaan Generative AI

Pengerjaan tugas ini dibantu oleh **Claude (Anthropic)** melalui claude.ai.
Penggunaan meliputi: debugging error build dan Docker networking, serta refactoring
struktur project dari multi-module menjadi single module.
