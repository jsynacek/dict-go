package main

import (
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"reflect"
	"runtime"
)

type Word struct {
	Word      string
	Phonetics []Phonetic
	Meanings  []Meaning
}

type Phonetic struct {
	Text  string
	Audio string
}

type Meaning struct {
	PartOfSpeech string
	Definitions  []Definition
	Synonyms     []string
	Antonyms     []string
}

type Definition struct {
	Definition string
	Synonyms   []string
	Antonyms   []string
	Example    string
}

type ErrorResponse struct {
	Title   string
	Message string
}

type AppContext struct {
	CacheDir string
	Words    []Word
	Template *template.Template
	Error    *ErrorResponse
}

func searchWord(word string, app *AppContext) {
	log.Print("asking: ", word)
	cacheFile := path.Join(app.CacheDir, word)
	if cacheFile != word {
		data, err := os.ReadFile(cacheFile)
		if err == nil {
			log.Print("cache hit: ", cacheFile)
			if e := json.Unmarshal(data, &app.Words); e != nil {
				log.Fatal(e)
			}
			return
		}
		if os.IsNotExist(err) {
			log.Print("cache miss: ", cacheFile)
		} else {
			log.Print("failed to read cache file: ", cacheFile)
		}
	}

	const baseUrl = "https://api.dictionaryapi.dev/api/v2/entries/en/"
	resp, err := http.Get(baseUrl + word)
	if err != nil {
		log.Printf("failed to GET %s: %s", baseUrl, err)
		return
	}
	defer resp.Body.Close()

	jsonData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Print("failed to read response body: ", err)
		return
	}
	log.Print("response status code: ", resp.Status)
	if !strings.HasPrefix(resp.Status, "20") {
		var eResp ErrorResponse
		if e := json.Unmarshal(jsonData, &eResp); e != nil {
			log.Fatal(e)
		}
		app.Error = &eResp
		app.Error.Title += " — " + word
		return
	}

	// Cache the result.
	if cacheFile != word {
		log.Print("caching: ", word)
		err = os.WriteFile(cacheFile, jsonData, 0644)
		if err != nil {
			log.Print("failed to write cache: %s", err)
		}
	}
	if e := json.Unmarshal(jsonData, &app.Words); e != nil {
		log.Fatal(e)
	}
}

// initCacheDir initializes the cache directory and returns its path.
// The cache directory is created if it does not exist. The path is either the value of
// $XDG_CACHE_HOME/godict or $HOME/.cache/godict.
// If the initialization fails, an empty string is returned, indicating that caching is
// disabled.
func initCacheDir() string {
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		cacheDir = os.Getenv("HOME")
		if cacheDir == "" {
			log.Fatal("$HOME not set")
		}
		cacheDir = path.Join(cacheDir, ".cache")
	}
	cacheDir = path.Join(cacheDir, "godict")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		log.Printf("failed to create cache dir: %s; ignoring", cacheDir)
		return ""
	}
	log.Print("cache dir: ", cacheDir)
	return cacheDir
}

// renderTemplate renders the main template.
func renderTemplate(w http.ResponseWriter, app *AppContext) {
	err := app.Template.Execute(w, app)
	if err != nil {
		log.Print("failed to execute template: ", err)
		http.Error(w, "Oops", http.StatusInternalServerError)
	}
}

// handleWithRateLimit wraps handler with a rate limiter.
// The rate is limited to one request per second.
func handleWithRateLimit(handler func(_ http.ResponseWriter, _ *http.Request)) func(_ http.ResponseWriter, _ *http.Request) {
	limiter := time.Tick(time.Second)
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-limiter:
			handler(w, r)
		default:
			// Ain't nothing like a bit of runtime reflection of function pointers!
			h := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
			log.Printf("%s: rate limit exceeded", h)
		}
	}
}

// handleRoot handles requests to "/".
func handleRoot(tmpl *template.Template) func(_ http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		renderTemplate(w, &AppContext{Template: tmpl})
	}
}

// handleSearch handles requests to "/search".
// It takes the word to search for from the "word" query argument.
func handleSearch(tmpl *template.Template, cacheDir string) func(_ http.ResponseWriter, _ *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		word := req.FormValue("word")
		app := AppContext{CacheDir: cacheDir, Template: tmpl}
		log.Print("handle search: ", word)
		if word == "" {
			http.Redirect(w, req, "/", http.StatusSeeOther)
			return
		}
		searchWord(word, &app)
		renderTemplate(w, &app)
	}
}

func handleStatic(w http.ResponseWriter, r *http.Request) {
	log.Print("serving static file: ", r.URL.Path)

	// Do a simple whitelist check first.
	whitelist := map[string]bool{"/static/dict.css": true}
	if !whitelist[r.URL.Path] {
		log.Print("static file not whitelisted: ", r.URL.Path)
		http.Error(w, "Oops", http.StatusNotFound)
		return
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Print("failed to get working directory")
		http.Error(w, "Oops", http.StatusInternalServerError)
		return
	}
	path := path.Join(wd, r.URL.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Print("failed to read file: ", path)
		http.Error(w, "Oops", http.StatusInternalServerError)
		return
	}
	w.Write(data)
}

func main() {
	log.Default().SetFlags(log.Ldate | log.Lmicroseconds | log.Lshortfile)
	templates := template.Must(template.ParseFiles("templates/main.tmpl"))
	cacheDir := initCacheDir()
	http.HandleFunc("/", handleWithRateLimit(handleRoot(templates)))
	http.HandleFunc("/search", handleWithRateLimit(handleSearch(templates, cacheDir)))
	http.HandleFunc("/static/", handleWithRateLimit(handleStatic))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
