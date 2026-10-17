# Arsitektur NineGuard

Dokumen ini menjelaskan rancangan sistem, alur transmisi paket, dan komponen inti dari **NineGuard**.

---

## 1. Konsep Desain

NineGuard dirancang berdasarkan prinsip:
* **Single Gateway for All Models:** AI Coding Agent hanya perlu mengarah ke satu endpoint (`http://localhost:8080/v1`).
* **Strict Client Authentication:** Hanya agen atau developer yang memiliki API key resmi dari NineGuard (`sk-ng-...`) yang diizinkan mengakses model.
* **Prefix-Based Upstream Routing:** Model dari berbagai penyedia dikelompokkan dengan prefix (misalnya `openrouter/model-id`, `local/model-id`).
* **Zero Overhead Streaming:** Response Server-Sent Events (SSE) dialirkan tanpa buffering berlebih.

---

## 2. Alur Request Masuk (Request Lifecycle)

```text
[1. Client Agent]
       │
       │ HTTP POST /v1/chat/completions
       │ Header: Authorization: Bearer sk-ng-...
       ▼
[2. NineGuard Security Interceptor]
       │
       ├── Cek Validitas NineGuard API Key
       │     └── Jika Invalid / Disabled ──► 401 Unauthorized
       │
       ├── Cek Key Quota (hanya mode "key", total semua model)
       │     └── Jika limit tercapai ───────► 429 Too Many Requests
       │
       ├── Cek Izin Model Per-Key (Allowed Models)
       │     └── Jika Model Tidak Diizinkan ──► 403 Forbidden
       │
       ├── Cek Status Model Firewall Global
       │     └── Jika Disabled ─────────────► 403 Forbidden
       │
       ├── Prefix Stripping & Provider Matching
       │     └── "openrouter/anthropic/claude-3.5-sonnet"
       │           ├── Prefix "openrouter" ──► Provider OpenRouter
       │           └── Model "anthropic/claude-3.5-sonnet"
       │
       ├── Cek Model Quota (hanya mode "model", pakai Model ID hasil resolve)
       │     └── Jika limit tercapai ───────► 429 model_quota_exceeded
       │
       ├── Rewrite Request Header:
       │     └── Authorization: Bearer <Master_API_Key_Provider>
       ▼
[3. Upstream Provider (OpenAI-Compatible)]
       │
       │ Eksekusi Inferensi LLM
       ▼
[4. Response Streaming & Trace Ingestion]
       ├── Stream chunk SSE langsung di-flush ke client (zero delay)
       └── Asynchronous Logging: simpan token, durasi, status ke SQLite
```

---

## 3. Komponen Inti

1. **`internal/proxy`:**
   * Mengelola multiplexing permintaan HTTP, parsing JSON request, dan streaming SSE.
   * Menghitung penggunaan token (Prompt & Completion Tokens) secara transparan.
2. **`internal/providers`:**
   * Mengelola registrasi provider upstream (Route, Master Key, Prefix, Default Fallback).
   * Menjalankan agregasi model multi-provider untuk endpoint `GET /v1/models`.
3. **`internal/keys`:**
   * Mengelola siklus hidup API key client NineGuard (`sk-ng-...`).
   * Melakukan verifikasi token cepat berbasis memory cache.
4. **`internal/models`:**
   * Bertindak sebagai firewall model. Model yang dinonaktifkan langsung dicegat sebelum menyentuh upstream.
5. **`internal/traffic`:**
   * Mengagregasi metrik analitik: data time-series, p95 latensi, token mix, dan laporan penggunaan per key.
   * Menghitung jendela kuota (`CalcQuotaWindow`) dari Quota Reset Schedule milik key. Lihat bagian 3.1.

### 3.1 Jendela Kuota & Quota Reset Schedule

Kuota (Key Quota maupun Model Quota) dihitung dari token yang terpakai di dalam **jendela** berjalan. Kapan jendela itu reset ditentukan oleh **Quota Reset Schedule**: timezone (nama IANA), jam (HH:MM), hari dalam minggu (untuk `weekly`), dan tanggal (untuk `monthly`). Lihat ADR 0010.

**Jadwal yang berlaku** untuk sebuah key dicari berurutan:
1. jadwal milik key itu sendiri (override),
2. jadwal global (diatur di halaman Quotas, tombol *Reset schedule*),
3. bawaan lama: 00:00 UTC, Senin untuk weekly, tanggal 1 untuk monthly.

Jadwal global kosong secara bawaan, jadi key yang sudah ada tidak berubah perilakunya sampai admin mengaturnya.

**Cara jendela dihitung.** Tidak ada penghitung atau cron reset. Awal jendela dihitung ulang setiap pengecekan dari jadwal dan jam sekarang, lalu token dijumlahkan dari `traffic_logs` sejak awal jendela itu (`SELECT SUM(total_tokens) ... WHERE timestamp >= awal_jendela`). Awal dan akhir tiap jendela berasal dari satu fungsi yang sama, sehingga jendela berurutan tidak pernah tumpang tindih atau berlubang. Waktu dibentuk dari tanggal lokal (`time.Date`), bukan penambahan 24 jam, jadi tidak bergeser satu jam saat daylight saving berganti. Tanggal 29-31 di bulan yang lebih pendek berarti hari terakhir bulan itu.

**Mengubah jadwal tidak menghitung ulang pemakaian.** Saat jadwal efektif sebuah key berubah (edit jadwal key, edit jadwal global, atau menghapus override), jendela yang sedang berjalan disimpan sebagai **Transitional Window**: awalnya tetap, dan berakhir di reset pertama dari jadwal baru. Setelah itu jendela kembali dihitung normal. Satu jendela itu bisa lebih pendek atau lebih panjang dari biasanya. Ini disimpan di kolom `api_keys.quota_window_pins` (JSON per periode, berisi `start` dan `until`) dan kedaluwarsa sendiri, tanpa job pembersih. Contoh: reset bulanan tanggal 1 diganti ke tanggal 20 pada 17 Okt, maka jendela menjadi 1 Okt sampai 20 Okt, lalu 20 Okt sampai 20 Nov. Pemakaian 4,1M tetap 4,1M, sehingga key yang sedang diblokir tidak terbuka dan key yang aman tidak mendadak terblokir.

**Penyimpanan:** `api_keys.quota_reset_schedule` (JSON, NULL berarti mengikuti jadwal global), `api_keys.quota_window_pins` (JSON), dan baris `settings` bernama `quota_reset_schedule` untuk jadwal global.

**Pesan 429** menampilkan jam reset dalam timezone jadwal efektif key (mis. `at 14:00 WIB`), atau `UTC` bila memakai bawaan. `Retry-After` dalam detik sampai reset berikutnya, atau sampai akhir Transitional Window.

---

## 4. Keamanan & Isolasi

* Master API Key dari upstream provider disimpan secara aman di SQLite lokal (`data/nineguard.db`) dan tidak pernah dikirim ke client agent.
* Client agent hanya berinteraksi menggunakan key `sk-ng-...`. Jika sebuah laptop atau environment terkompromi, key client tersebut dapat dicabut seketika tanpa perlu mengganti master key di provider upstream.
