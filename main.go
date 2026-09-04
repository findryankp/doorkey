// doorkey adalah web server yang menyediakan endpoint untuk buka/tutup pintu
// via browser, lalu meneruskan perintahnya ke ESP32 lewat HTTP di jaringan lokal.
// Endpoint /access adalah jalur backup: mencatat siapa yang masuk, mengirim
// notifikasi WA ke grup, lalu tetap membuka pintu lewat ESP32 yang sama.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"time"
)

var (
	espIP      string
	waURL      string
	waGroupID  string
	ruang      string
	httpClient = &http.Client{Timeout: 5 * time.Second}
	waktuLoc   *time.Location
)

const waMessageTemplate = "🔐 AKSES RUANGAN 🔐\n" +
	"━━━━━━━━━━━━━━━━━━━━\n" +
	"🚪 Ruang : %s\n" +
	"👤 Nama : %s\n" +
	"🆔 Bagian: %s\n" +
	"⏰ Waktu : %s\n" +
	"━━━━━━━━━━━━━━━━━━━━\n" +
	"Akses ruangan berhasil dicatat oleh sistem."

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

type accessRequest struct {
	Noreg  string `json:"noreg"`
	Nama   string `json:"nama"`
	Bagian string `json:"bagian"`
}

func kirimWA(message string) error {
	payload, _ := json.Marshal(map[string]string{
		"group_id": waGroupID,
		"message":  message,
	})

	resp, err := httpClient.Post(waURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
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
	flag.StringVar(&espIP, "esp-ip", "192.168.0.237", "alamat IP ESP32 di jaringan lokal")
	flag.StringVar(&waURL, "wa-url", "http://192.168.0.135:8006/wablas/send-message-to-group", "endpoint wablas untuk kirim pesan grup")
	flag.StringVar(&waGroupID, "wa-group", "120363422555070122@g.us", "group_id WA tujuan notifikasi")
	flag.StringVar(&ruang, "ruang", "Sparepart", "nama ruangan yang dikontrol pintu ini")
	port := flag.String("port", "8018", "port web server ini")
	flag.Parse()

	var err error
	waktuLoc, err = time.LoadLocation("Asia/Jakarta")
	if err != nil {
		waktuLoc = time.FixedZone("WIB", 7*60*60)
	}

	tmpl := template.Must(template.New("page").Parse(pageTemplate))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl.Execute(w, nil)
	})

	http.HandleFunc("/door", func(w http.ResponseWriter, r *http.Request) {
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
	})

	// endpoint backup: mencatat identitas + kirim notifikasi WA, lalu tetap membuka ESP32 yang sama
	http.HandleFunc("/access", func(w http.ResponseWriter, r *http.Request) {
		req := accessRequest{
			Noreg:  r.URL.Query().Get("noreg"),
			Nama:   r.URL.Query().Get("nama"),
			Bagian: r.URL.Query().Get("bagian"),
		}
		if req.Noreg == "" || req.Nama == "" || req.Bagian == "" {
			http.Error(w, "noreg, nama, dan bagian wajib diisi", http.StatusBadRequest)
			return
		}

		waktu := time.Now().In(waktuLoc).Format("02 Jan 2006, 15:04:05 WIB")
		message := fmt.Sprintf(waMessageTemplate, ruang, req.Nama, req.Bagian, waktu)

		if err := kirimWA(message); err != nil {
			log.Println("gagal mengirim notifikasi WA:", err)
		}

		espResp, err := bukaESP32()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		log.Println("Akses dicatat:", req.Noreg, "-", req.Nama, "-", req.Bagian)
		fmt.Fprintf(w, "Pintu dibuka untuk %s (%s, %s). Balasan ESP32: %s", req.Nama, req.Noreg, req.Bagian, espResp)
	})

	addr := ":" + *port
	log.Printf("Server berjalan di http://localhost%s (meneruskan ke ESP32 di %s)\n", addr, espIP)
	log.Fatal(http.ListenAndServe(addr, nil))
}
