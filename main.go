// doorkey adalah web server yang menyediakan endpoint untuk buka/tutup pintu
// via browser, lalu meneruskan perintahnya ke ESP32 lewat HTTP di jaringan lokal.
// Endpoint /access adalah jalur backup: mencatat siapa yang masuk, mengirim
// notifikasi WA ke grup, lalu tetap membuka pintu lewat ESP32 yang sama.
package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	espIP      string
	waURL      string
	waGroupIDs []string
	ruang      string
	dbPath     string
	httpClient = &http.Client{Timeout: 5 * time.Second}
	waktuLoc   *time.Location
	db         *sql.DB
)

const waMessageTemplate = "🔐 AKSES RUANGAN 🔐\n" +
	"━━━━━━━━━━━━━━━━━━━━\n" +
	"🚪 Ruang : %s\n" +
	"👤 Nama : %s\n" +
	"🆔 Bagian: %s\n" +
	"⏰ Waktu : %s\n" +
	"━━━━━━━━━━━━━━━━━━━━\n" +
	"Akses ruangan berhasil dicatat oleh sistem.\n"+
	"_`#p5 #accesskey #sparepart #others`_"

const pageTemplate = `<!DOCTYPE html>
<html lang="id">
<head>
	<meta charset="UTF-8">
	<title>Kontrol Pintu</title>
	<style>
		body { font-family: sans-serif; text-align: center; margin-top: 60px; }
		button {
			font-size: 20px; padding: 16px 32px; margin: 10px;
			border: none; border-radius: 8px; color: white; cursor: pointer;
		}
		.buka { background: #2e7d32; }
		.tutup { background: #c62828; }
		.akses { background: #1565c0; }
		#status { margin-top: 20px; font-size: 18px; color: #333; white-space: pre-wrap; }
		.box { max-width: 360px; margin: 30px auto; padding: 20px; border: 1px solid #ccc; border-radius: 8px; }
		.box input { font-size: 18px; padding: 8px; width: 90%; margin: 6px 0; }
		.box label { display: block; text-align: left; font-size: 13px; color: #666; margin-top: 8px; }
		.info { text-align: left; font-size: 14px; color: #444; margin-top: 6px; min-height: 20px; }
	</style>
</head>
<body>
	<h1>Kontrol Pintu</h1>

	<div class="box">
		<h3>Akses Ruangan (Backup)</h3>
		<label>Nomor Registrasi (Noreg)</label>
		<input type="text" id="noreg" placeholder="cth: OTS0035" oninput="cariKaryawan()">
		<div class="info" id="infoKaryawan"></div>
		<button class="akses" id="btnAkses" style="display:none" onclick="kirimAkses()">BUKA & CATAT AKSES</button>
	</div>

	<p id="status"></p>

	<p><a href="/riwayat">Lihat Riwayat Akses</a></p>

	<script>
		const karyawan = {
			"0404914": { nama: "Heri Kiswanto", bagian: "Security" },
			"0509956": { nama: "Dwi Hartanto", bagian: "Security" },
			"0510959": { nama: "Akhmad Taufik", bagian: "Security" },
			"1202113": { nama: "Ali Mubin", bagian: "Security" },
			"OTS0035": { nama: "Fersa Dwi Septian", bagian: "Security" },
			"OTS4468": { nama: "Akhmad Faishol Afandi", bagian: "Security" },
			"OTS6038": { nama: "M. Hadi", bagian: "Security" },
			"OTS6039": { nama: "Risky Ramadan", bagian: "Security" },
			"1701W02": { nama: "Yura Bagus Nanda Tama", bagian: "Production" },
			"1402W09": { nama: "Nixtian Arry Perdhana", bagian: "Maintenance" },
			"1506W04": { nama: "Oky Wahyu Pratama", bagian: "Maintenance" },
			"2607122": { nama: "Alvin Pramana Putra", bagian: "Admin Sparepart" }
		};

		function cariKaryawan() {
			const noreg = document.getElementById('noreg').value.trim().toUpperCase();
			const info = document.getElementById('infoKaryawan');
			const btn = document.getElementById('btnAkses');
			const data = karyawan[noreg];
			if (data) {
				info.innerText = 'Nama: ' + data.nama + ' | Bagian: ' + data.bagian;
				btn.style.display = 'inline-block';
			} else {
				info.innerText = noreg ? 'Noreg tidak ditemukan' : '';
				btn.style.display = 'none';
			}
		}

		async function kirimAkses() {
			const noreg = document.getElementById('noreg').value.trim().toUpperCase();
			const data = karyawan[noreg];
			if (!data) {
				document.getElementById('status').innerText = 'Noreg tidak ditemukan, tidak bisa membuka pintu.';
				return;
			}
			document.getElementById('status').innerText = 'Mengirim...';
			try {
				const params = new URLSearchParams({ noreg: noreg, nama: data.nama, bagian: data.bagian });
				const res = await fetch('/access?' + params.toString());
				const text = await res.text();
				document.getElementById('status').innerText = text;
			} catch (e) {
				document.getElementById('status').innerText = 'Gagal: ' + e;
			}
		}
	</script>
</body>
</html>`

const riwayatTemplate = `<!DOCTYPE html>
<html lang="id">
<head>
	<meta charset="UTF-8">
	<title>Riwayat Akses</title>
	<style>
		body { font-family: sans-serif; text-align: center; margin: 40px; }
		.filter { margin-bottom: 20px; }
		.filter input { font-size: 16px; padding: 6px; }
		.filter button { font-size: 15px; padding: 6px 14px; margin-left: 6px; cursor: pointer; }
		table { border-collapse: collapse; margin: 0 auto; min-width: 700px; }
		th, td { border: 1px solid #ccc; padding: 8px 14px; text-align: left; }
		th { background: #1565c0; color: white; }
		tr:nth-child(even) { background: #f5f5f5; }
		#empty { color: #777; margin-top: 20px; }
	</style>
</head>
<body>
	<h1>Riwayat Akses (20 Terbaru)</h1>

	<div class="filter">
		<label>Filter Tanggal: </label>
		<input type="date" id="tanggal" onchange="muatLog()">
		<button onclick="resetFilter()">Reset</button>
	</div>

	<table id="tabelLog">
		<thead>
			<tr>
				<th>Waktu</th>
				<th>Nama</th>
				<th>Noreg</th>
				<th>Bagian</th>
				<th>Ruang</th>
			</tr>
		</thead>
		<tbody id="isiTabel"></tbody>
	</table>
	<p id="empty" style="display:none">Tidak ada data.</p>

	<p><a href="/">Kembali ke Kontrol Pintu</a></p>

	<script>
		async function muatLog() {
			const tanggal = document.getElementById('tanggal').value;
			const res = await fetch('/api/logs' + (tanggal ? ('?tanggal=' + tanggal) : ''));
			const data = await res.json();

			const tbody = document.getElementById('isiTabel');
			tbody.innerHTML = '';
			document.getElementById('empty').style.display = data.length ? 'none' : 'block';

			for (const log of data) {
				const tr = document.createElement('tr');
				tr.innerHTML = '<td>' + log.waktu + '</td>' +
					'<td>' + log.nama + '</td>' +
					'<td>' + log.noreg + '</td>' +
					'<td>' + log.bagian + '</td>' +
					'<td>' + log.ruang + '</td>';
				tbody.appendChild(tr);
			}
		}

		function resetFilter() {
			document.getElementById('tanggal').value = '';
			muatLog();
		}

		muatLog();
	</script>
</body>
</html>`

type accessRequest struct {
	Noreg  string `json:"noreg"`
	Nama   string `json:"nama"`
	Bagian string `json:"bagian"`
}

func kirimWA(message string) error {
	if len(waGroupIDs) == 0 {
		return fmt.Errorf("tidak ada group_id WA yang dikonfigurasi")
	}

	var errs []string
	for _, groupID := range waGroupIDs {
		payload, err := json.Marshal(map[string]string{
			"group_id": groupID,
			"message":  message,
		})
		if err != nil {
			errs = append(errs, fmt.Sprintf("grup %s (marshal error): %v", groupID, err))
			continue
		}

		resp, err := httpClient.Post(waURL, "application/json", bytes.NewReader(payload))
		if err != nil {
			errs = append(errs, fmt.Sprintf("grup %s: %v", groupID, err))
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			errs = append(errs, fmt.Sprintf("grup %s status %d: %s", groupID, resp.StatusCode, string(body)))
			continue
		}
		resp.Body.Close()
		log.Printf("Notifikasi WA berhasil dikirim ke grup: %s\n", groupID)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// loadEnv membaca file format KEY=VALUE sederhana dan menyimpannya ke environment jika belum disetel
func loadEnv(filepath string) {
	file, err := os.Open(filepath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			val = strings.Trim(val, `"'`)
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func getEnvAny(defaultVal string, keys ...string) string {
	for _, key := range keys {
		if val := os.Getenv(key); val != "" {
			return val
		}
	}
	return defaultVal
}

func parseGroupIDs(raw string) []string {
	var list []string
	for _, item := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

func initDB(path string) error {
	var err error
	db, err = sql.Open("sqlite", path)
	if err != nil {
		return err
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS akses_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		nama TEXT NOT NULL,
		noreg TEXT NOT NULL,
		bagian TEXT NOT NULL,
		ruang TEXT NOT NULL,
		waktu TEXT NOT NULL
	)`)
	return err
}

func catatAkses(nama, noreg, bagian, ruangAkses string, waktu time.Time) error {
	_, err := db.Exec(
		`INSERT INTO akses_log (nama, noreg, bagian, ruang, waktu) VALUES (?, ?, ?, ?, ?)`,
		nama, noreg, bagian, ruangAkses, waktu.Format("2006-01-02 15:04:05"),
	)
	return err
}

type accessLog struct {
	Nama   string `json:"nama"`
	Noreg  string `json:"noreg"`
	Bagian string `json:"bagian"`
	Ruang  string `json:"ruang"`
	Waktu  string `json:"waktu"`
}

// ambilLog mengembalikan 20 log terbaru, atau 20 log terbaru pada tanggal
// tertentu (format tanggal "2006-01-02") kalau tanggal diisi.
func ambilLog(tanggal string) ([]accessLog, error) {
	rows, err := db.Query(
		`SELECT nama, noreg, bagian, ruang, waktu FROM akses_log
		 WHERE ? = '' OR substr(waktu, 1, 10) = ?
		 ORDER BY waktu DESC LIMIT 20`,
		tanggal, tanggal,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := []accessLog{}
	for rows.Next() {
		var l accessLog
		if err := rows.Scan(&l.Nama, &l.Noreg, &l.Bagian, &l.Ruang, &l.Waktu); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

func withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func bukaESP32() (string, error) {
	url := fmt.Sprintf("http://%s/door?state=1", espIP)
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ESP32 membalas status %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

func main() {
	loadEnv(".env")

	defaultESP := getEnvAny("192.168.0.237", "ESP_IP")
	defaultWAURL := getEnvAny("http://192.168.0.135:8006/wablas/send-message-to-group", "WA_URL")
	defaultWAGroups := getEnvAny("120363422555070122@g.us", "WA_GROUP_IDS", "WA_GROUPS", "WA_GROUP")
	defaultRuang := getEnvAny("Sparepart", "RUANG")
	defaultDBPath := getEnvAny("doorkey.db", "DB_PATH")
	defaultPort := getEnvAny("8018", "PORT")

	var waGroupsRaw string
	flag.StringVar(&espIP, "esp-ip", defaultESP, "alamat IP ESP32 di jaringan lokal")
	flag.StringVar(&waURL, "wa-url", defaultWAURL, "endpoint wablas untuk kirim pesan grup")
	flag.StringVar(&waGroupsRaw, "wa-groups", defaultWAGroups, "group_id WA tujuan notifikasi (pisahkan koma jika lebih dari 1 grup)")
	flag.StringVar(&waGroupsRaw, "wa-group", defaultWAGroups, "alias untuk -wa-groups")
	flag.StringVar(&ruang, "ruang", defaultRuang, "nama ruangan yang dikontrol pintu ini")
	flag.StringVar(&dbPath, "db-path", defaultDBPath, "path file database SQLite untuk log akses")
	port := flag.String("port", defaultPort, "port web server ini")
	flag.Parse()

	waGroupIDs = parseGroupIDs(waGroupsRaw)

	var err error
	waktuLoc, err = time.LoadLocation("Asia/Jakarta")
	if err != nil {
		waktuLoc = time.FixedZone("WIB", 7*60*60)
	}

	if err := initDB(dbPath); err != nil {
		log.Fatal("gagal membuka database:", err)
	}
	defer db.Close()

	tmpl := template.Must(template.New("page").Parse(pageTemplate))
	riwayatTmpl := template.Must(template.New("riwayat").Parse(riwayatTemplate))

	http.HandleFunc("/", withCORS(func(w http.ResponseWriter, r *http.Request) {
		tmpl.Execute(w, nil)
	}))

	http.HandleFunc("/riwayat", withCORS(func(w http.ResponseWriter, r *http.Request) {
		riwayatTmpl.Execute(w, nil)
	}))

	http.HandleFunc("/api/logs", withCORS(func(w http.ResponseWriter, r *http.Request) {
		tanggal := r.URL.Query().Get("tanggal")
		logs, err := ambilLog(tanggal)
		if err != nil {
			http.Error(w, fmt.Sprintf("gagal mengambil log: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(logs)
	}))

	http.HandleFunc("/door", withCORS(func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state != "1" && state != "0" {
			http.Error(w, "state harus 1 (buka) atau 0 (tutup)", http.StatusBadRequest)
			return
		}

		url := fmt.Sprintf("http://%s/door?state=%s", espIP, state)
		resp, err := httpClient.Get(url)
		if err != nil {
			http.Error(w, fmt.Sprintf("gagal menghubungi ESP32: %v", err), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			http.Error(w, fmt.Sprintf("ESP32 membalas status %d: %s", resp.StatusCode, string(body)), http.StatusBadGateway)
			return
		}

		action := "TUTUP"
		if state == "1" {
			action = "BUKA"
		}
		log.Println("Perintah", action, "berhasil dikirim. Balasan ESP32:", string(body))
		fmt.Fprintf(w, "Perintah %s berhasil dikirim. Balasan ESP32: %s", action, string(body))
	}))

	// endpoint backup: mencatat identitas + kirim notifikasi WA, lalu tetap membuka ESP32 yang sama
	http.HandleFunc("/access", withCORS(func(w http.ResponseWriter, r *http.Request) {
		req := accessRequest{
			Noreg:  r.URL.Query().Get("noreg"),
			Nama:   r.URL.Query().Get("nama"),
			Bagian: r.URL.Query().Get("bagian"),
		}
		if req.Noreg == "" || req.Nama == "" || req.Bagian == "" {
			http.Error(w, "noreg, nama, dan bagian wajib diisi", http.StatusBadRequest)
			return
		}

		waktuSekarang := time.Now().In(waktuLoc)
		message := fmt.Sprintf(waMessageTemplate, ruang, req.Nama, req.Bagian, waktuSekarang.Format("02 Jan 2006, 15:04:05 WIB"))

		if err := kirimWA(message); err != nil {
			log.Println("gagal mengirim notifikasi WA:", err)
		}

		espResp, err := bukaESP32()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		if err := catatAkses(req.Nama, req.Noreg, req.Bagian, ruang, waktuSekarang); err != nil {
			log.Println("gagal menyimpan log akses ke database:", err)
		}

		log.Println("Akses dicatat:", req.Noreg, "-", req.Nama, "-", req.Bagian)
		fmt.Fprintf(w, "Pintu dibuka untuk %s (%s, %s). Balasan ESP32: %s", req.Nama, req.Noreg, req.Bagian, espResp)
	}))

	addr := ":" + *port
	log.Printf("Server berjalan di http://localhost%s (meneruskan ke ESP32 di %s)\n", addr, espIP)
	log.Printf("Notifikasi WA dikonfigurasi ke %d grup: %v\n", len(waGroupIDs), waGroupIDs)
	log.Fatal(http.ListenAndServe(addr, nil))
}
