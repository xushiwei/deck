// sex: slide execution
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	port    = flag.String("port", ":1958", "http service address")
	deckdir = flag.String("dir", ".", "directory for decks")
	deckrun = false
	deckpid int
)

const (
	timeformat  = "Jan 2, 2006, 3:04pm (MST)"
	filepattern = "\\.xml$|\\.mov$|\\.mp4$|\\.m4v$|\\.avi$"
)

type layout struct {
	x     float64
	align string
}

func main() {
	flag.Parse()
	err := os.Chdir(*deckdir)
	if err != nil {
		log.Fatal("Set Directory:", err)
	}
	log.Print("Startup...")
	http.Handle("/deck/", http.HandlerFunc(deck))
	http.Handle("/upload/", http.HandlerFunc(upload))
	http.Handle("/table/", http.HandlerFunc(table))
	http.Handle("/media/", http.HandlerFunc(media))

	err = http.ListenAndServe(*port, nil)
	if err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

// writeresponse writes a string to a ResponseWriter
func writeresponse(w http.ResponseWriter, s string) {
	io.WriteString(w, s)
}

// eresp sends the client a JSON encoded error
func eresp(w http.ResponseWriter, err string) {
	writeresponse(w, fmt.Sprintf("{\"error\": \"%s\"}\n", err))
}

// deckinfo returns information (file, size, date) for a deck and movie files in the deck directory
func deckinfo(w http.ResponseWriter, data []os.FileInfo, pattern string) {
	writeresponse(w, `{"decks":[`)
	nf := 0
	for _, s := range data {
		matched, err := regexp.MatchString(pattern, s.Name())
		if err == nil && matched {
			nf++
			if nf > 1 {
				writeresponse(w, ",\n")
			}
			writeresponse(w, fmt.Sprintf(`{"name":"%s", "size":%d, "date":"%s"}`,
				s.Name(), s.Size(), s.ModTime().Format(timeformat)))
		}
	}
	writeresponse(w, "]}\n")
}

// maketable creates a deck file from a tab separated list
// that includes a specification in the first record
func maketable(w io.Writer, r io.Reader) {
	y := 90.0
	linespacing := 8.0
	textsize := 3.0
	tightness := 3.5
	showrule := true

	l := make([]layout, 10)
	fmt.Fprintf(w, "<deck><slide>\n")
	scanner := bufio.NewScanner(r)
	for nr := 0; scanner.Scan(); nr++ {
		data := scanner.Text()
		fields := strings.Split(data, "\t")
		nf := len(fields)
		if nf > 10 || nf < 1 {
			nf = 10
		}
		if nr == 0 {
			for i := 0; i < nf; i++ {
				c := strings.Split(fields[i], ":")
				if len(c) != 2 {
					return
				}
				x, _ := strconv.ParseFloat(c[0], 64)
				l[i].x = x
				l[i].align = c[1]
			}
		} else {
			ty := y - (linespacing / tightness)
			for i := 0; i < nf; i++ {
				fmt.Fprintf(w, "<text xp=\"%g\" yp=\"%g\" sp=\"%g\" align=\"%s\">%s</text>\n",
					l[i].x, y, textsize, l[i].align, fields[i])
			}
			if showrule {
				fmt.Fprintf(w, "<line xp1=\"%g\" yp1=\"%.2f\" xp2=\"%g\" yp2=\"%.2f\" sp=\"0.05\"/>\n",
					l[0].x, ty, l[nf-1].x+5, ty)
			}
		}
		y -= linespacing
	}
	fmt.Fprintf(w, "</slide></deck>\n")
}

// table makes a table from POSTed data
// POST /table, Deck:<input>
func table(w http.ResponseWriter, req *http.Request) {
	requester := req.RemoteAddr
	w.Header().Set("Content-Type", "application/json")
	if req.Method == "POST" {
		defer req.Body.Close()
		deckpath := req.Header.Get("Deck")
		if deckpath == "" {
			w.WriteHeader(500)
			eresp(w, "table: no deckpath")
			log.Printf("%s table error: no deckpath", requester)
			return
		}
		f, err := os.Create(deckpath)
		if err != nil {
			w.WriteHeader(500)
			eresp(w, err.Error())
			log.Printf("%s %v", requester, err)
			return
		}
		maketable(f, req.Body)
		f.Close()
		writeresponse(w, fmt.Sprintf("{\"table\":\"%s\"}\n", deckpath))
		log.Printf("%s table %s", requester, deckpath)
	}
}

// upload uploads decks from POSTed data
// POST /upload, Deck:<file>
func upload(w http.ResponseWriter, req *http.Request) {
	requester := req.RemoteAddr
	w.Header().Set("Content-Type", "application/json")
	if req.Method == "POST" || req.Method == "PUT" {
		deckpath := req.Header.Get("Deck")
		if deckpath == "" {
			w.WriteHeader(500)
			eresp(w, "upload: no deckpath")
			log.Printf("%s upload error: no deckpath", requester)
			return
		}
		deckdata, err := ioutil.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(500)
			eresp(w, err.Error())
			log.Printf("%s %v", requester, err)
			return
		}
		defer req.Body.Close()
		err = ioutil.WriteFile(deckpath, deckdata, 0644)
		if err != nil {
			w.WriteHeader(500)
			eresp(w, err.Error())
			log.Printf("%s %v", requester, err)
			return
		}
		writeresponse(w, fmt.Sprintf("{\"upload\":\"%s\"}\n", deckpath))
		log.Printf("%s write: %#v, %d bytes", requester, deckpath, len(deckdata))
	}
}

// media plays video
// POST /media Media:<file>
func media(w http.ResponseWriter, req *http.Request) {
	requester := req.RemoteAddr
	w.Header().Set("Content-Type", "application/json")
	media := req.Header.Get("Media")
	log.Printf("%s media: running %s", requester, media)
	command := exec.Command("omxplayer", "-o", "both", media)
	err := command.Start()
	if err != nil {
		log.Printf("%s %v", requester, err)
		w.WriteHeader(500)
		eresp(w, err.Error())
		return
	}
	deckpid = command.Process.Pid
	log.Printf("%s video: %#v, pid: %d", requester, media, deckpid)
	writeresponse(w, fmt.Sprintf("{\"deckpid\":\"%d\", \"media\":\"%s\"}\n", deckpid, media))
}

// deck processes slide decks
// GET /deck  -- list information
// POST /deck/file.xml?cmd=[duration] -- starts a deck
// POST /deck?cmd=stop -- stops a deck
// DELETE /deck/file.xml  --  removes a deck
func deck(w http.ResponseWriter, req *http.Request) {
	requester := req.RemoteAddr
	w.Header().Set("Content-Type", "application/json")
	query := req.URL.Query()
	deck := path.Base(req.URL.Path)
	p, ok := query["cmd"]
	var param string
	if ok {
		param = p[0]
	}
	method := req.Method
	postflag := method == "POST" && len(param) > 0 && deck != "deck"
	log.Printf("%s %s %#v %#v", requester, method, deck, param)
	switch {
	case postflag && !deckrun && param != "stop":
		if deck == "" {
			w.WriteHeader(406)
			eresp(w, "deck: need a deck")
			return
		}
		command := exec.Command("vgdeck", "-loop", param, deck)
		err := command.Start()
		if err != nil {
			log.Printf("%s %v", requester, err)
			w.WriteHeader(500)
			eresp(w, err.Error())
			return
		}
		deckpid = command.Process.Pid
		deckrun = true
		log.Printf("%s deck: %#v, duration: %#v, pid: %d", requester, deck, param, deckpid)
		writeresponse(w, fmt.Sprintf("{\"deckpid\":\"%d\", \"deck\":\"%s\", \"duration\":\"%s\"}\n", deckpid, deck, param))
		return
	case postflag && deckrun && param == "stop":
		kp, err := os.FindProcess(deckpid)
		if err != nil {
			w.WriteHeader(500)
			eresp(w, err.Error())
			log.Printf("%s %v", requester, err)
			return
		}
		err = kp.Kill()
		if err != nil {
			w.WriteHeader(500)
			eresp(w, err.Error())
			log.Printf("%s %v", requester, err)
			return
		}
		log.Printf("%s stop %d", requester, deckpid)
		writeresponse(w, fmt.Sprintf("{\"stop\":\"%d\"}\n", deckpid))
		deckrun = false
		return
	case method == "GET":
		f, err := os.Open(*deckdir)
		if err != nil {
			log.Printf("%s %v", requester, err)
			w.WriteHeader(500)
			eresp(w, err.Error())
			return
		}
		names, err := f.Readdir(-1)
		if err != nil {
			log.Printf("%s %v", requester, err)
			w.WriteHeader(500)
			eresp(w, err.Error())
			return
		}
		log.Printf("%s list decks", requester)
		deckinfo(w, names, filepattern)
		return
	case method == "DELETE" && deck != "deck":
		if deck == "" {
			log.Printf("%s delete error: specify a name", requester)
			w.WriteHeader(406)
			eresp(w, "deck delete: specify a name")
			return
		}
		err := os.Remove(deck)
		if err != nil {
			log.Printf("%s %v", requester, err)
			w.WriteHeader(500)
			eresp(w, err.Error())
			return
		}
		writeresponse(w, fmt.Sprintf("{\"remove\":\"%s\"}\n", deck))
		log.Printf("%s remove %s", requester, deck)
		return
	}
}