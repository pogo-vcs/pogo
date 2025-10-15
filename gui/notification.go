package gui

import (
	"fmt"
	"log"

	"github.com/gen2brain/beeep"
	"github.com/pogo-vcs/pogo/brand"
)

func Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if err := beeep.Notify("Pogo error", msg, brand.LogoPng); err != nil {
		log.Printf("Failed to send notification: %v", err)
	} else {
		log.Printf("Notification sent %q", msg)
	}
}