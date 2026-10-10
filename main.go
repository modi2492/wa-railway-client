
package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

const qrPath = "/tmp/wa-qr.png"

func handleEvent(evt any) {
	v, ok := evt.(*events.Message)
	if !ok {
		return
	}

	// تشخيص كل رسالة قبل شروط الفلترة.
	fmt.Printf(
		"[DEBUG] fromMe=%v group=%v chat=%s sender=%s id=%s\n",
		v.Info.IsFromMe,
		v.Info.IsGroup,
		v.Info.Chat,
		v.Info.Sender,
		v.Info.ID,
	)

	// استقبال الرسائل النصية من محادثة المستخدم مع نفسه.
	if v.Info.IsGroup || !v.Info.IsFromMe ||
    v.Info.Chat != v.Info.Sender.ToNonAD() {
    return
 }

	var text string

	if v.Message.GetConversation() != "" {
		text = v.Message.GetConversation()
	} else if msg := v.Message.GetExtendedTextMessage(); msg != nil {
		text = msg.GetText()
	}

	if text == "" {
		return
	}

	fmt.Printf(
		"[CHAT] bytes=%d chars=%d text_received=true\n",
		len(text),
		utf8.RuneCountInString(text),
	)
}

func startQRServer() {
	token := os.Getenv("QR_TOKEN")
	if token == "" {
		panic("QR_TOKEN environment variable is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/qr.png", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		provided := r.URL.Query().Get("token")
		if len(provided) != len(token) ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		info, err := os.Stat(qrPath)
		if err != nil || info.IsDir() {
			http.Error(w, "QR is not available", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, qrPath)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "WhatsApp Railway client is running.")
	})

	go func() {
		fmt.Println("HTTP server listening on port", port)
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			fmt.Println("HTTP server error:", err)
		}
	}()
}

func main() {
	ctx := context.Background()

	dbPath := os.Getenv("WHATSAPP_DB_PATH")
	if dbPath == "" {
		dbPath = "/data/whatsmeow.db"
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		panic(err)
	}

	container, err := sqlstore.New(
		ctx,
		"sqlite3",
		"file:"+dbPath+"?_foreign_keys=on&_busy_timeout=5000",
		nil,
	)
	if err != nil {
		panic(err)
	}
	defer container.Close()

	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		panic(err)
	}

	client := whatsmeow.NewClient(
		device,
		waLog.Stdout("WA", "INFO", true),
	)
	client.AddEventHandler(handleEvent)

	startQRServer()

	if client.Store.ID == nil {
		qrChan, err := client.GetQRChannel(ctx)
		if err != nil {
			panic(err)
		}

		if err := client.Connect(); err != nil {
			panic(err)
		}

		for evt := range qrChan {
			switch evt.Event {
			case "code":
				cmd := exec.Command(
					"qrencode",
					"-o", qrPath,
					"-s", "8",
					"-m", "2",
					evt.Code,
				)

				if err := cmd.Run(); err != nil {
					fmt.Println("QR generation failed:", err)
				} else {
					fmt.Println("QR image ready at /qr.png")
				}

			case "success":
				_ = os.Remove(qrPath)
				fmt.Println("WhatsApp linked successfully.")
			case "timeout":
				_ = os.Remove(qrPath)
				fmt.Println("QR expired. Waiting for another code.")
			default:
				fmt.Println("Login event:", evt.Event)
			}
		}
	} else {
		if err := client.Connect(); err != nil {
			panic(err)
		}
	}

	fmt.Println("WhatsApp client running.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	_ = os.Remove(qrPath)
	client.Disconnect()
}
