package main

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"html/template"

	"github.com/nfnt/resize"
)

func main() {
	// Static files must be registered first to avoid being caught by the root handler
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static/"))))
	http.HandleFunc("/", serveForm)
	http.HandleFunc("/upload", handleUpload)

	// Get port from environment variable (for deployment) or default to 8080
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Server started at http://localhost:%s\n", port)
	http.ListenAndServe(":"+port, nil)
}

func serveForm(w http.ResponseWriter, r *http.Request) {
	// Only serve the root path, return 404 for everything else
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, "Template parsing error", http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func handleUpload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20) // 10MB max

	file, handler, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	img, format, err := image.Decode(file)
	if err != nil {
		http.Error(w, "Error decoding image", http.StatusInternalServerError)
		return
	}

	newImg := resize.Resize(uint(img.Bounds().Dx()/2), 0, img, resize.Lanczos3)

	compressedFile := "compressed_" + handler.Filename
	out, err := os.Create(compressedFile)
	if err != nil {
		http.Error(w, "Error creating output file", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	switch strings.ToLower(format) {
	case "jpeg", "jpg":
		jpeg.Encode(out, newImg, &jpeg.Options{Quality: 50})
	case "png":
		png.Encode(out, newImg)
	default:
		http.Error(w, "Unsupported format", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(compressedFile))
	w.Header().Set("Content-Type", "application/octet-stream")

	f, err := os.Open(compressedFile)
	if err != nil {
		http.Error(w, "Error reading output", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	io.Copy(w, f)

	// Optional cleanup:
	// os.Remove(compressedFile)
}
