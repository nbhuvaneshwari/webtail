package main

import (
	"bufio"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	filePeriod = 10 * time.Millisecond
)

var (
	addr      = flag.String("addr", ":8080", "http service address")
	homeTempl = template.Must(template.New("").Parse(homeHTML))
	filenames []string
	stdIn     bool
	upgrader  = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

func reader(ws *websocket.Conn) {
	defer ws.Close()
	ws.SetReadLimit(512)
	ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error { ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, p, err := ws.ReadMessage()
		log.Println("msg from client", string(p))
		if err != nil {
			break
		}
	}
}

func writer(ws *websocket.Conn, fn string) {
	pingTicker := time.NewTicker(pingPeriod)
	fileTicker := time.NewTicker(filePeriod)
	defer func() {
		pingTicker.Stop()
		fileTicker.Stop()
		ws.Close()
	}()
	var r *bufio.Reader
	if !stdIn {
		log.Println("Selected file:", fn)
		f, _ := os.Open(fn)
		r = bufio.NewReader(f)
		defer f.Close()
	} else {
		r = bufio.NewReader(os.Stdin)
	}

	for {
		select {
		case <-fileTicker.C:
			p, _ := r.ReadBytes('\n')

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
	fn := r.FormValue("file")
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			log.Println(err)
		}
		return
	}

	go writer(ws, fn)
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
		Host      string
		Data      string
		FileNames []string
	}{
		r.Host,
		string(p),
		filenames,
	}
	homeTempl.Execute(w, &v)
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		stdIn = true
	} else {
		fnames := flag.Args()
		for _, fn := range fnames {
			p, _ := filepath.Abs(fn)
			filenames = append(filenames, p)
		}
	}
	http.HandleFunc("/", serveHome)
	http.HandleFunc("/ws", serveWs)
	log.Println("Listening on", *addr)
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
    File: <select id="fileName">
	  {{ range $i, $fn := .FileNames }}
	     <option value="{{ $fn }}">{{ $fn }}</option>
	  {{ end }}
	</select>
        <option>
	</option>
        <div id="fileData"><pre>{{.Data}}</pre></div>
        <script type="text/javascript">
            (function() {
                var data = $("#fileData");
		var val = $("select option:selected").val();
		var conn = new WebSocket("ws://{{.Host}}/ws?file=" + val);
		conn.onclose = onclose
		conn.onmessage = onmessage
                var onclose = function myclose (evt) {
                    data.text('Connection closed');
		    var val = $("select option:selected").val();
		    conn = new WebSocket("ws://{{.Host}}/ws?file=" + val);
		    conn.onclose = myclose;
		    conn.onmessage = onmessage;
                };
                var onmessage = function(evt) {
		    if (evt.data != "") {
                        console.log('file updated');
                        data.append("<pre>"+evt.data+"</pre>");
		    }
                };
		$("#fileName").change(function() {
		    conn.close()
		    data.empty();
		});
            })();
        </script>
    </body>
</html>
`
