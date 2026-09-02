// doorkey adalah web server yang menyediakan endpoint untuk buka/tutup pintu
// via browser, lalu meneruskan perintahnya ke ESP32 lewat HTTP di jaringan lokal.
package main

import (
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
	httpClient = &http.Client{Timeout: 5 * time.Second}
)

const pageTemplate = `<!DOCTYPE html>
<html lang="id">
<head>
	<meta charset="UTF-8">
	<title>Kontrol Pintu</title>
	<style>
		body { font-family: sans-serif; text-align: center; margin-top: 80px; }
		button {
			font-size: 24px; padding: 20px 40px; margin: 10px;
			border: none; border-radius: 8px; color: white; cursor: pointer;
		}
		.buka { background: #2e7d32; }
		.tutup { background: #c62828; }
		#status { margin-top: 20px; font-size: 18px; color: #333; }
	</style>
</head>
<body>
	<h1>Kontrol Pintu</h1>
	<button class="buka" onclick="kirim('1')">BUKA</button>
	<button class="tutup" onclick="kirim('0')">TUTUP</button>
	<p id="status"></p>

	<script>
		async function kirim(state) {
			document.getElementById('status').innerText = 'Mengirim...';
			try {
				const res = await fetch('/door?state=' + state);
				const text = await res.text();
				document.getElementById('status').innerText = text;
			} catch (e) {
				document.getElementById('status').innerText = 'Gagal: ' + e;
			}
		}
	</script>
</body>
</html>`

func main() {
	flag.StringVar(&espIP, "esp-ip", "192.168.0.237", "alamat IP ESP32 di jaringan lokal")
	port := flag.String("port", "8018", "port web server ini")
	flag.Parse()

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

	addr := ":" + *port
	log.Printf("Server berjalan di http://localhost%s (meneruskan ke ESP32 di %s)\n", addr, espIP)
	log.Fatal(http.ListenAndServe(addr, nil))
}
