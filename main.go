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
	"runtime"
	"strings"

	"html/template"
	"os/exec"

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
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		fmt.Printf("Server error: %v\n", err)
	}
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
	r.ParseMultipartForm(100 << 20) // 100MB max

	file, handler, err := r.FormFile("image")
	if err != nil {
		fmt.Println("Error retrieving the file:", err)
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Save uploaded file to disk
	uploadPath := "upload_" + handler.Filename
	out, err := os.Create(uploadPath)
	if err != nil {
		fmt.Println("Error saving upload:", err)
		http.Error(w, "Error saving upload", http.StatusInternalServerError)
		return
	}
	_, err = io.Copy(out, file)
	out.Close()
	if err != nil {
		fmt.Println("Error saving upload:", err)
		http.Error(w, "Error saving upload", http.StatusInternalServerError)
		return
	}

	ext := strings.ToLower(filepath.Ext(handler.Filename))
	var compressedFile string
	// Video and GIF compression using ffmpeg
	if ext == ".mp4" || ext == ".mov" || ext == ".avi" || ext == ".webm" || ext == ".mkv" || ext == ".gif" {
		compressedFile = "compressed_" + handler.Filename
		// ffmpeg command for video/gif compression (high quality, optimized size)
		// For GIF: palette for better quality
		// FFmpeg path - works on both Windows and Linux
		ffmpegPath := "ffmpeg"
		if runtime.GOOS == "windows" {
			// Check if local ffmpeg exists
			if _, err := os.Stat(`C:\ffmpeg\ffmpeg.exe`); err == nil {
				ffmpegPath = `C:\ffmpeg\ffmpeg.exe`
			}
		}
		var ffmpegCmd string
		if ext == ".gif" {
			// Optimized GIF compression for smaller file size
			palette := "palette.png"
			// Step 1: Generate optimized palette with fewer colors (fixed with -update flag)
			paletteCmd := fmt.Sprintf(`%s -y -i %s -vf fps=15,scale=iw*0.6:ih*0.6:flags=bilinear,palettegen=max_colors=128:stats_mode=single -update 1 %s`, ffmpegPath, uploadPath, palette)
			// Step 2: Apply palette with size optimization
			gifCmd := fmt.Sprintf(`%s -y -i %s -i %s -lavfi fps=15,scale=iw*0.6:ih*0.6:flags=bilinear[x];[x][1:v]paletteuse=dither=none %s`, ffmpegPath, uploadPath, palette, compressedFile)
			ffmpegCmd = fmt.Sprintf(`%s && %s`, paletteCmd, gifCmd)
		} else {
			// Ultra high quality video compression with optimized settings
			passLogFile := "ffmpeg2pass"
			nullDevice := "/dev/null"
			if runtime.GOOS == "windows" {
				nullDevice = "NUL"
			}
			// First pass for analysis with higher bitrate target
			pass1Cmd := fmt.Sprintf(`%s -y -i %s -vf scale=iw*0.85:ih*0.85:flags=lanczos -c:v libx264 -preset veryslow -crf 20 -b:v 2000k -maxrate 3000k -bufsize 4000k -pass 1 -passlogfile %s -f null %s`, ffmpegPath, uploadPath, passLogFile, nullDevice)
			// Second pass with premium settings
			pass2Cmd := fmt.Sprintf(`%s -y -i %s -vf scale=iw*0.85:ih*0.85:flags=lanczos -c:v libx264 -preset veryslow -crf 20 -b:v 2000k -maxrate 3000k -bufsize 4000k -pass 2 -passlogfile %s -c:a aac -b:a 192k -ac 2 -ar 48000 %s`, ffmpegPath, uploadPath, passLogFile, compressedFile)
			ffmpegCmd = fmt.Sprintf(`%s && %s`, pass1Cmd, pass2Cmd)
		}
		err = runFFmpeg(ffmpegCmd)
		if err != nil {
			fmt.Println("Error compressing video/gif:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			// Don't remove files yet, let user debug
			return
		}
	} else {
		// Image compression (existing logic)
		inFile, err := os.Open(uploadPath)
		if err != nil {
			fmt.Println("Error opening upload:", err)
			http.Error(w, "Error opening upload", http.StatusInternalServerError)
			return
		}
		img, format, err := image.Decode(inFile)
		inFile.Close()
		if err != nil {
			fmt.Println("Error decoding image:", err)
			http.Error(w, "Error decoding image", http.StatusInternalServerError)
			return
		}
		newImg := resize.Resize(uint(img.Bounds().Dx()*3/4), 0, img, resize.Lanczos3)
		compressedFile = "compressed_" + handler.Filename
		out, err := os.Create(compressedFile)
		if err != nil {
			fmt.Println("Error creating output file:", err)
			http.Error(w, "Error creating output file", http.StatusInternalServerError)
			return
		}
		switch strings.ToLower(format) {
		case "jpeg", "jpg":
			jpeg.Encode(out, newImg, &jpeg.Options{Quality: 85}) // Higher quality
		case "png":
			png.Encode(out, newImg)
		default:
			fmt.Println("Unsupported format:", format)
			http.Error(w, "Unsupported format", http.StatusBadRequest)
			out.Close()
			return
		}
		out.Close()
	}

	w.Header().Set("Content-Disposition", "attachment; filename="+filepath.Base(compressedFile))
	w.Header().Set("Content-Type", "application/octet-stream")

	f, err := os.Open(compressedFile)
	if err != nil {
		fmt.Println("Error reading output:", err)
		http.Error(w, "Error reading output", http.StatusInternalServerError)
		return
	}
	defer func() {
		f.Close()
		os.Remove(uploadPath)
		os.Remove(compressedFile)
		os.Remove("palette.png")              // for GIFs
		os.Remove("ffmpeg2pass-0.log")        // for 2-pass video encoding
		os.Remove("ffmpeg2pass-0.log.mbtree") // for 2-pass video encoding
	}()
	io.Copy(w, f)
}

// runFFmpeg runs the given ffmpeg shell command and returns error output if any
func runFFmpeg(cmd string) error {
	var c *exec.Cmd
	if os.Getenv("OS") == "Windows_NT" {
		c = exec.Command("cmd", "/C", cmd)
	} else {
		c = exec.Command("sh", "-c", cmd)
	}
	output, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg error: %v\nOutput: %s", err, string(output))
	}
	return nil
}
