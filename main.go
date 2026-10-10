
package main

import (
	"context"
	"fmt"
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

func handleEvent(evt any) {
	v, ok := evt.(*events.Message)
	if !ok {
		return
	}

	// نختبر الرسائل النصية في محادثة المستخدم مع نفسه فقط.
	if v.Info.IsGroup || !v.Info.IsFromMe ||
		v.Info.Chat != v.Info.Sender {
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
				fmt.Println("امسح QR من WhatsApp > الأجهزة المرتبطة:")

				cmd := exec.Command(
					"qrencode", "-t", "ANSIUTF8", evt.Code,
				)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr

				if err := cmd.Run(); err != nil {
					fmt.Println("تعذر عرض QR:", err)
					fmt.Println("QR data:", evt.Code)
				}

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

	fmt.Println("Disconnecting...")
	client.Disconnect()
}
