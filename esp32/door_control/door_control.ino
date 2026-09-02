// Firmware ESP32: menerima perintah HTTP GET /door?state=1|0
// dan menggerakkan relay/servo untuk buka-tutup pintu.
#include <WiFi.h>
#include <WebServer.h>

const char *ssid = "NAMA_WIFI_KAMU";
const char *password = "PASSWORD_WIFI_KAMU";

const int RELAY_PIN = 26; // sesuaikan dengan pin yang dipakai untuk relay/servo

WebServer server(80);

void setup() {
  Serial.begin(115200);

  pinMode(RELAY_PIN, OUTPUT);
  digitalWrite(RELAY_PIN, LOW); // kondisi awal: pintu tertutup

  WiFi.begin(ssid, password);
  Serial.print("Menghubungkan ke WiFi");
  while (WiFi.status() != WL_CONNECTED) {
    delay(500);
    Serial.print(".");
  }
  Serial.println();
  Serial.print("Terhubung! IP ESP32: ");
  Serial.println(WiFi.localIP());

  server.on("/door", handleDoor);
  server.begin();
  Serial.println("HTTP server berjalan di port 80");
}

void loop() {
  server.handleClient();
}

void handleDoor() {
  if (!server.hasArg("state")) {
    server.send(400, "text/plain", "parameter state wajib diisi");
    return;
  }

  String state = server.arg("state");

  if (state == "1") {
    digitalWrite(RELAY_PIN, HIGH); // buka pintu
    Serial.println("pintu dibuka");
    server.send(200, "text/plain", "pintu dibuka");
  } else if (state == "0") {
    digitalWrite(RELAY_PIN, LOW); // tutup pintu
    Serial.println("pintu ditutup");
    server.send(200, "text/plain", "pintu ditutup");
  } else {
    server.send(400, "text/plain", "state harus 1 atau 0");
  }
}
