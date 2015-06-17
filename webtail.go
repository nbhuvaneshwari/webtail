package main

import (
	"bufio"
	"flag"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write the file to the client.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the client.
	pongWait = 60 * time.Second

	// Send pings to client with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 20

	// Poll file for changes with this period.
	filePeriod = 200 * time.Millisecond
)

var (
	addr      = flag.String("addr", ":8080", "http service address")
	homeTempl = template.Must(template.New("").Parse(homeHTML))
	filename  string
	upgrader  = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

func readFile(f *os.File) ([]byte, error) {
	scanner := bufio.NewScanner(f)
	var out string

	for scanner.Scan() {
		out = out + "\n" + scanner.Text()
	}

	if err := scanner.Err(); err != nil {
		return []byte(out), err
	}
	return []byte(out), nil
}

func reader(ws *websocket.Conn) {
	defer ws.Close()
	ws.SetReadLimit(512)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			break
		}
	}
}

func writer(ws *websocket.Conn) {
	lastError := ""
	pingTicker := time.NewTicker(pingPeriod)
	fileTicker := time.NewTicker(filePeriod)
	defer func() {
		pingTicker.Stop()
		fileTicker.Stop()
		ws.Close()
	}()
	f, _ := os.Open(filename)
	defer f.Close()
	for {
		select {
		case <-fileTicker.C:
			var p []byte
			var err error

			p, err = readFile(f)
			if err != nil {
				if s := err.Error(); s != lastError {
					lastError = s
					p = []byte(lastError)
				}
			} else {
				lastError = ""
			}

			if p != nil {
				ws.SetWriteDeadline(time.Now().Add(writeWait))
				if err := ws.WriteMessage(websocket.TextMessage, p); err != nil {
					return
				}
			}
		case <-pingTicker.C:
			ws.SetWriteDeadline(time.Now().Add(writeWait))
			if err := ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			log.Println(err)
		}
		return
	}

	go writer(ws)
	reader(ws)
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "Not found", 404)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	p := ""
	var v = struct {
		Host string
		Data string
	}{
		r.Host,
		string(p),
	}
	homeTempl.Execute(w, &v)
}

func main() {
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("filename not specified")
	}
	filename = flag.Args()[0]
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", serveWs)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}

const homeHTML = `<!DOCTYPE html>
<html lang="en">
    <head>
        <title>WebSocket Example</title>
	<script src="https://ajax.googleapis.com/ajax/libs/jquery/2.1.4/jquery.min.js"></script>
        <style>
          pre {
            margin: 0;
            padding: 0;
          }
        </style>
    </head>
    <body>
        <div id="fileData"><pre>{{.Data}}</pre></div>
        <script type="text/javascript">
            (function() {
                var data = $("#fileData");
                var conn = new WebSocket("ws://{{.Host}}/ws");
                conn.onclose = function(evt) {
                    data.text('Connection closed');
                }
                conn.onmessage = function(evt) {
                    console.log('file updated');
                    data.append("<pre>"+evt.data+"</pre>");
                }
            })();
        </script>
    </body>
</html>
`